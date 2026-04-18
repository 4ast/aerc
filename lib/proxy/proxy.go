package proxy

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func DialWithProxy(network, addr string, timeout time.Duration) (net.Conn, error) {
	proxyURL := proxyFromEnv()
	if proxyURL == nil {
		return dialDirect(network, addr, timeout)
	}
	return dialViaProxy(proxyURL, addr, timeout)
}

func proxyFromEnv() *url.URL {
	for _, env := range []string{"AERC_PROXY", "ALL_PROXY", "HTTPS_PROXY", "all_proxy", "https_proxy"} {
		if v := os.Getenv(env); v != "" {
			u, err := url.Parse(v)
			if err == nil {
				return u
			}
		}
	}
	return nil
}

func dialDirect(network, addr string, timeout time.Duration) (net.Conn, error) {
	if timeout > 0 {
		return net.DialTimeout(network, addr, timeout)
	}
	return net.Dial(network, addr)
}

func dialViaProxy(proxyURL *url.URL, targetAddr string, timeout time.Duration) (net.Conn, error) {
	proxyHost := proxyURL.Host
	if !strings.Contains(proxyHost, ":") {
		switch proxyURL.Scheme {
		case "https":
			proxyHost += ":443"
		default:
			proxyHost += ":8080"
		}
	}

	var proxyConn net.Conn
	var err error

	if timeout > 0 {
		proxyConn, err = net.DialTimeout("tcp", proxyHost, timeout)
	} else {
		proxyConn, err = net.Dial("tcp", proxyHost)
	}
	if err != nil {
		return nil, fmt.Errorf("proxy connect: %w", err)
	}

	if proxyURL.Scheme == "https" {
		tlsConfig := &tls.Config{
			ServerName:         strings.Split(proxyHost, ":")[0],
			InsecureSkipVerify: true,
		}
		certFile := os.Getenv("AERC_PROXY_CERT")
		keyFile := os.Getenv("AERC_PROXY_KEY")
		if certFile != "" {
			if keyFile == "" {
				keyFile = certFile
			}
			cert, err := tls.LoadX509KeyPair(certFile, keyFile)
			if err != nil {
				proxyConn.Close()
				return nil, fmt.Errorf("proxy client cert: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		proxyConn = tls.Client(proxyConn, tlsConfig)
	}

	connectReq := fmt.Sprintf(
		"CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n",
		targetAddr, targetAddr,
	)
	if _, err := proxyConn.Write([]byte(connectReq)); err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("proxy CONNECT write: %w", err)
	}

	br := bufio.NewReader(proxyConn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		proxyConn.Close()
		return nil, fmt.Errorf("proxy CONNECT response: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != 200 {
		proxyConn.Close()
		return nil, fmt.Errorf("proxy CONNECT failed: %s", resp.Status)
	}

	if br.Buffered() > 0 {
		return &bufferedConn{Conn: proxyConn, br: br}, nil
	}
	return proxyConn, nil
}

type bufferedConn struct {
	net.Conn
	br *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) {
	return c.br.Read(b)
}
