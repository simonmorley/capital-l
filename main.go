package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"sync"

	"golang.org/x/time/rate"
)

var limiter = NewIPRateLimiter(1, 1)

// https://medium.com/@pliutau/rate-limiting-http-requests-in-go-based-on-ip-address-4e66d1bea4cf
// From there, thanks for now!
type IPRateLimiter struct {
	ips map[string]*rate.Limiter
	mu  *sync.RWMutex
	r   rate.Limit
	b   int
}

// NewIPRateLimiter .
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	i := &IPRateLimiter{
		ips: make(map[string]*rate.Limiter),
		mu:  &sync.RWMutex{},
		r:   r,
		b:   b,
	}

	return i
}

// AddIP creates a new rate limiter and adds it to the ips map,
// using the IP address as the key
func (i *IPRateLimiter) AddIP(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	limiter := rate.NewLimiter(i.r, i.b)

	i.ips[ip] = limiter

	return limiter
}

// GetLimiter returns the rate limiter for the provided IP address if it exists.
// Otherwise calls AddIP to add IP address to the map
func (i *IPRateLimiter) GetLimiter(r *http.Request) *rate.Limiter {

	ip := RequestIP(r)

	i.mu.Lock()
	limiter, exists := i.ips[ip]

	if !exists {
		i.mu.Unlock()
		return i.AddIP(ip)
	}

	i.mu.Unlock()

	return limiter
}

func RequestIP(r *http.Request) string {
	if proxyHost := r.Header.Get("CF-Connecting-IP"); proxyHost != "" {
		return proxyHost
	}

	if proxyHost := r.Header.Get("X-Forwarded-For"); proxyHost != "" {
		return proxyHost
	}

	return r.RemoteAddr
}

func Hijack(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
		return
	}

	// Hijack the connection
	conn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, "Failed to hijack connection", http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// Send a custom response
	response := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\n\r\n"
	conn.Write([]byte(response))
	return
}

func proxy(w http.ResponseWriter, r *http.Request) {
	buf, _ := ioutil.ReadAll(r.Body)

	proxy := os.Getenv("TARGET_URL")
	if proxy == "" {
		proxy = "http://127.0.0.1:8081"
	}

	proxy = fmt.Sprintf("%s%s", proxy, r.URL.Path)
	target, err := url.Parse(proxy)

	if err != nil {
		http.Error(w, "Error parsing target URL", http.StatusInternalServerError)
		return
	}

	// Add query parameters from the original request
	target.RawQuery = r.URL.RawQuery

	proxyReq, err := http.NewRequest(r.Method, target.String(), bytes.NewReader(buf))

	// Add original headers
	proxyReq.Header = r.Header
	proxyReq.Header = make(http.Header)
	for h, val := range r.Header {
		proxyReq.Header[h] = val
	}

	client := &http.Client{}
	resp, err := client.Do(proxyReq)

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	defer resp.Body.Close()

	buff, _ := ioutil.ReadAll(resp.Body)

	// if blocked(r) {
	// 	Hijack(w) // !!!
	// 	return
	// }

	w.Write(buff)
	return
}

func LimiterMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limiter := limiter.GetLimiter(r)
		if !limiter.Allow() {
			// http.Error(w, http.StatusText(http.StatusTooManyRequests), http.StatusTooManyRequests)
			Hijack(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func main() {
	fmt.Print("Starting....")

	mux := http.NewServeMux()

	mux.HandleFunc("/", proxy)

	// http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
	// 	proxy(w, r)
	// })

	if err := http.ListenAndServe(":8080", LimiterMiddleware(mux)); err != nil {
		log.Fatal(err)
	}

	// if err := http.ListenAndServe(":8080", nil); err != nil {
	// }
}
