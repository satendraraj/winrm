package winrm

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/satendraraj/winrm/soap"
)

// ClientAuthRequest ClientAuthRequest
type ClientAuthRequest struct {
	transport http.RoundTripper
	dial      func(network, addr string) (net.Conn, error)
}

// Transport Transport
func (c *ClientAuthRequest) Transport(endpoint *Endpoint) error {
	cert, err := tls.X509KeyPair(endpoint.Cert, endpoint.Key)
	if err != nil {
		return err
	}

	dial := (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).Dial

	if c.dial != nil {
		dial = c.dial
	}

	//nolint:gosec
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			Renegotiation:      tls.RenegotiateOnceAsClient,
			InsecureSkipVerify: endpoint.Insecure,
			Certificates:       []tls.Certificate{cert},
			MaxVersion:         tls.VersionTLS12,
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

// parse func reads the response body and return it as a string
func parse(response *http.Response) (string, error) {
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

// Post Post
func (c ClientAuthRequest) Post(client *Client, request *soap.SoapMessage) (string, error) {
	httpClient := &http.Client{Transport: c.transport}

	req, err := http.NewRequest("POST", client.url, strings.NewReader(request.String()))
	if err != nil {
		return "", fmt.Errorf("impossible to create http request %w", err)
	}

	req.Header.Set("Content-Type", soapXML+";charset=UTF-8")
	req.Header.Set("Authorization", "http://schemas.dmtf.org/wbem/wsman/1/wsman/secprofile/https/mutual")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("unknown error %w", err)
	}

	body, err := parse(resp)
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

// NewClientAuthRequestWithDial NewClientAuthRequestWithDial
func NewClientAuthRequestWithDial(dial func(network, addr string) (net.Conn, error)) *ClientAuthRequest {
	return &ClientAuthRequest{
		dial: dial,
	}
}
