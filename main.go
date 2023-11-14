package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
)

func blocked(ip string) bool {
	// get the blocked ips
	return false
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

	if blocked(r.RemoteAddr) {
		Hijack(w) // !!!
		return
	}

	w.Write(buff)
	return
}

func main() {
	fmt.Print("Starting....")

	http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		proxy(w, r)
	})

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}
