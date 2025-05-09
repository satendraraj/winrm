package winrm

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/satendraraj/winrm/soap"
)

var soapXML = "application/soap+xml"

// body func reads the response body and return it as a string
func body(response *http.Response) (string, error) {
	// if we received the content we expected
	if strings.Contains(response.Header.Get("Content-Type"), "application/soap+xml") {
		body, err := io.ReadAll(response.Body)
		defer func() {
			// defer can modify the returned value before
			// it is actually passed to the calling statement
			if errClose := response.Body.Close(); errClose != nil && err == nil {
				err = errClose
			}
		}()
		if err != nil {
			return "", fmt.Errorf("error while reading request body %w", err)
		}

		return string(body), nil
	}

	return "", fmt.Errorf("invalid content type")
}

type clientRequest struct {
	transport http.RoundTripper
	dial      func(network, addr string) (net.Conn, error)
	proxyfunc func(req *http.Request) (*url.URL, error)
}

func (c *clientRequest) Transport(endpoint *Endpoint) error {
	dial := (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).Dial

	if c.dial != nil {
		dial = c.dial
	}

	proxyfunc := http.ProxyFromEnvironment
	if c.proxyfunc != nil {
		proxyfunc = c.proxyfunc
	}

	//nolint:gosec
	transport := &http.Transport{
		Proxy: proxyfunc,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: endpoint.Insecure,
			ServerName:         endpoint.TLSServerName,
		},
		Dial:                  dial,
		ResponseHeaderTimeout: endpoint.Timeout,
	}

	if endpoint.CACert != nil && len(endpoint.CACert) > 0 {
		certPool, err := readCACerts(endpoint.CACert)
		if err != nil {
			return err
		}

		transport.TLSClientConfig.RootCAs = certPool
	}

	c.transport = transport

	return nil
}

// Post make post to the winrm soap service
func (c clientRequest) Post(client *Client, request *soap.SoapMessage) (string, error) {
	httpClient := &http.Client{Transport: c.transport}

	//nolint:noctx
	req, err := http.NewRequest("POST", client.url, strings.NewReader(request.String()))
	if err != nil {
		return "", fmt.Errorf("impossible to create http request %w", err)
	}
	req.Header.Set("Content-Type", soapXML+";charset=UTF-8")
	req.SetBasicAuth(client.username, client.password)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("unknown error %w", err)
	}

	body, err := body(resp)
	if err != nil {
		return "", fmt.Errorf("http response error: %d - %w", resp.StatusCode, err)
	}

	// if we have different 200 http status code
	// we must replace the error
	defer func() {
		if resp.StatusCode != 200 {
			body, err = "", fmt.Errorf("http error %d: %s", resp.StatusCode, body)
		}
	}()

	return body, err
}

// NewClientWithDial NewClientWithDial
func NewClientWithDial(dial func(network, addr string) (net.Conn, error)) *clientRequest {
	return &clientRequest{
		dial: dial,
	}
}

// NewClientWithProxyFunc NewClientWithProxyFunc
func NewClientWithProxyFunc(proxyfunc func(req *http.Request) (*url.URL, error)) *clientRequest {
	return &clientRequest{
		proxyfunc: proxyfunc,
	}
}
