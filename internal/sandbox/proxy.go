package sandbox

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
)

const (
	innerHTTPPort  = 16921
	innerSOCKSPort = 16922
)

type netBridge struct {
	HTTPSock, SOCKSSock string
	HTTPPort, SOCKSPort int
	HostTCP             bool
}

var (
	proxyMu   sync.Mutex
	proxyInst *runningProxy
	proxyNet  Network
)

type runningProxy struct {
	httpLn, socksLn net.Listener
	httpSock        string
	socksSock       string
	tcpHTTP         string
	tcpSOCKS        string
}

func currentNetwork() Network {
	proxyMu.Lock()
	defer proxyMu.Unlock()
	return proxyNet
}

func ensureProxy(netcfg Network) netBridge {
	if len(netcfg.AllowedDomains) == 0 {
		return netBridge{}
	}
	proxyMu.Lock()
	proxyNet = netcfg
	if proxyInst != nil {
		br := bridgeFrom(proxyInst)
		proxyMu.Unlock()
		return br
	}
	proxyMu.Unlock()

	inst, err := startProxy()
	if err != nil {
		return netBridge{}
	}
	proxyMu.Lock()
	if proxyInst == nil {
		proxyInst = inst
	} else {
		_ = inst.httpLn.Close()
		_ = inst.socksLn.Close()
		inst = proxyInst
	}
	br := bridgeFrom(inst)
	proxyMu.Unlock()
	return br
}

func bridgeFrom(inst *runningProxy) netBridge {
	if inst.tcpHTTP != "" {
		_, hp, _ := net.SplitHostPort(inst.tcpHTTP)
		_, sp, _ := net.SplitHostPort(inst.tcpSOCKS)
		hport, _ := strconv.Atoi(hp)
		sport, _ := strconv.Atoi(sp)
		return netBridge{HTTPPort: hport, SOCKSPort: sport, HostTCP: true}
	}
	return netBridge{
		HTTPSock: inst.httpSock, SOCKSSock: inst.socksSock,
		HTTPPort: innerHTTPPort, SOCKSPort: innerSOCKSPort,
	}
}

func startProxy() (*runningProxy, error) {
	if runtime.GOOS == "darwin" {
		httpLn, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		socksLn, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			_ = httpLn.Close()
			return nil, err
		}
		inst := &runningProxy{httpLn: httpLn, socksLn: socksLn, tcpHTTP: httpLn.Addr().String(), tcpSOCKS: socksLn.Addr().String()}
		go serveHTTPProxy(httpLn)
		go serveSOCKS(socksLn)
		return inst, nil
	}
	dir, err := os.MkdirTemp("", "pigo-sandbox-")
	if err != nil {
		return nil, err
	}
	httpSock := filepath.Join(dir, "http.sock")
	socksSock := filepath.Join(dir, "socks.sock")
	httpLn, err := net.Listen("unix", httpSock)
	if err != nil {
		return nil, err
	}
	socksLn, err := net.Listen("unix", socksSock)
	if err != nil {
		_ = httpLn.Close()
		return nil, err
	}
	_ = os.Chmod(httpSock, 0o600)
	_ = os.Chmod(socksSock, 0o600)
	inst := &runningProxy{httpLn: httpLn, socksLn: socksLn, httpSock: httpSock, socksSock: socksSock}
	go serveHTTPProxy(httpLn)
	go serveSOCKS(socksLn)
	return inst, nil
}

func serveHTTPProxy(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go handleHTTPProxy(c)
	}
}

func handleHTTPProxy(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}
	cfg := currentNetwork()
	if !HostAllowed(host, cfg.AllowedDomains, cfg.DeniedDomains) {
		_, _ = io.WriteString(c, "HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n")
		return
	}
	_ = c.SetDeadline(time.Time{})
	if req.Method == http.MethodConnect {
		remote, err := net.DialTimeout("tcp", host, 15*time.Second)
		if err != nil {
			_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
			return
		}
		defer func() { _ = remote.Close() }()
		_, _ = io.WriteString(c, "HTTP/1.1 200 Connection Established\r\n\r\n")
		relay(c, remote)
		return
	}
	outReq := req.Clone(req.Context())
	outReq.RequestURI = ""
	if outReq.URL.Scheme == "" {
		outReq.URL.Scheme = "http"
	}
	if outReq.URL.Host == "" {
		outReq.URL.Host = host
	}
	resp, err := http.DefaultTransport.RoundTrip(outReq)
	if err != nil {
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer func() { _ = resp.Body.Close() }()
	_ = resp.Write(c)
}

func serveSOCKS(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go handleSOCKS(c)
	}
}

func handleSOCKS(c net.Conn) {
	defer func() { _ = c.Close() }()
	_ = c.SetDeadline(time.Now().Add(30 * time.Second))
	buf := make([]byte, 258)
	n, err := io.ReadAtLeast(c, buf, 2)
	if err != nil || buf[0] != 0x05 {
		return
	}
	nmethods := int(buf[1])
	need := 2 + nmethods
	if n < need {
		if _, err := io.ReadFull(c, buf[n:need]); err != nil {
			return
		}
	}
	_, _ = c.Write([]byte{0x05, 0x00})
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil || hdr[0] != 0x05 || hdr[1] != 0x01 {
		return
	}
	var host string
	switch hdr[3] {
	case 0x01:
		ip := make([]byte, 4)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return
		}
		name := make([]byte, l[0])
		if _, err := io.ReadFull(c, name); err != nil {
			return
		}
		host = string(name)
	case 0x04:
		ip := make([]byte, 16)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = net.IP(ip).String()
	default:
		return
	}
	pb := make([]byte, 2)
	if _, err := io.ReadFull(c, pb); err != nil {
		return
	}
	port := binary.BigEndian.Uint16(pb)
	hostport := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	cfg := currentNetwork()
	if !HostAllowed(hostport, cfg.AllowedDomains, cfg.DeniedDomains) {
		_, _ = c.Write([]byte{0x05, 0x02, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	remote, err := net.DialTimeout("tcp", hostport, 15*time.Second)
	if err != nil {
		_, _ = c.Write([]byte{0x05, 0x05, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
		return
	}
	defer func() { _ = remote.Close() }()
	_, _ = c.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	_ = c.SetDeadline(time.Time{})
	relay(c, remote)
}

func relay(a, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
}
