package winrm

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/satendraraj/winrm/soap"
)

// Client struct
type Client struct {
	Parameters
	username string
	password string
	useHTTPS bool
	url      string
	http     Transporter
}

// Transporter does different transporters
// and init a Post request based on them
type Transporter interface {
	// init request baset on the transport configurations
	Post(*Client, *soap.SoapMessage) (string, error)
	Transport(*Endpoint) error
}

// NewClient will create a new remote client on url, connecting with user and password
// This function doesn't connect (connection happens only when CreateShell is called)
func NewClient(endpoint *Endpoint, user, password string) (*Client, error) {
	return NewClientWithParameters(endpoint, user, password, DefaultParameters)
}

// NewClientWithParameters will create a new remote client on url, connecting with user and password
// This function doesn't connect (connection happens only when CreateShell is called)
func NewClientWithParameters(endpoint *Endpoint, user, password string, params *Parameters) (*Client, error) {
	// alloc a new client
	client := &Client{
		Parameters: *params,
		username:   user,
		password:   password,
		url:        endpoint.url(),
		useHTTPS:   endpoint.HTTPS,
		// default transport
		http: &clientRequest{dial: params.Dial},
	}

	// switch to other transport if provided
	if params.TransportDecorator != nil {
		client.http = params.TransportDecorator()
	}

	// set the transport to some endpoint configuration
	if err := client.http.Transport(endpoint); err != nil {
		return nil, fmt.Errorf("can't parse this key and certs: %w", err)
	}

	return client, nil
}

func readCACerts(certs []byte) (*x509.CertPool, error) {
	certPool := x509.NewCertPool()

	if !certPool.AppendCertsFromPEM(certs) {
		return nil, errors.New("unable to read certificates")
	}

	return certPool, nil
}

// CreateShell will create a WinRM Shell,
// which is the prealable for running commands.
func (c *Client) CreateShell() (*Shell, error) {
	request := NewOpenShellRequest(c.url, &c.Parameters)
	defer request.Free()

	response, err := c.sendRequest(request)
	if err != nil {
		return nil, err
	}

	shellID, err := ParseOpenShellResponse(response)
	if err != nil {
		return nil, err
	}

	return c.NewShell(shellID), nil
}

// NewShell will create a new WinRM Shell for the given shellID
func (c *Client) NewShell(id string) *Shell {
	return &Shell{client: c, id: id}
}

// sendRequest exec the custom http func from the client
func (c *Client) sendRequest(request *soap.SoapMessage) (string, error) {
	return c.http.Post(c, request)
}

// Run will run command on the the remote host, writing the process stdout and stderr to
// the given writers. Note with this method it isn't possible to inject stdin.
//
// Deprecated: use RunWithContext()
func (c *Client) Run(command string, stdout io.Writer, stderr io.Writer) (int, error) {
	return c.RunWithContext(context.Background(), command, stdout, stderr)
}

// RunWithContext will run command on the the remote host, writing the process stdout and stderr to
// the given writers. Note with this method it isn't possible to inject stdin.
// If the context is canceled, the remote command is canceled.
func (c *Client) RunWithContext(ctx context.Context, command string, stdout io.Writer, stderr io.Writer) (int, error) {
	return c.RunWithContextWithInput(ctx, command, stdout, stderr, nil)
}

// RunWithString will run command on the the remote host, returning the process stdout and stderr
// as strings, and using the input stdin string as the process input
//
// Deprecated: use RunWithContextWithString()
func (c *Client) RunWithString(command string, stdin string) (string, string, int, error) {
	return c.RunWithContextWithString(context.Background(), command, stdin)
}

// RunWithContextWithString will run command on the the remote host, returning the process stdout and stderr
// as strings, and using the input stdin string as the process input
// If the context is canceled, the remote command is canceled.
func (c *Client) RunWithContextWithString(ctx context.Context, command string, stdin string) (string, string, int, error) {
	var outWriter, errWriter bytes.Buffer
	exitCode, err := c.RunWithContextWithInput(ctx, command, &outWriter, &errWriter, strings.NewReader(stdin))
	return outWriter.String(), errWriter.String(), exitCode, err
}

// RunCmdWithContext will run command on the the remote host, returning the process stdout and stderr
// as strings
// If the context is canceled, the remote command is canceled.
func (c *Client) RunCmdWithContext(ctx context.Context, command string) (string, string, int, error) {
	var outWriter, errWriter bytes.Buffer
	exitCode, err := c.RunWithContextWithInput(ctx, command, &outWriter, &errWriter, nil)
	return outWriter.String(), errWriter.String(), exitCode, err
}

// RunPSWithString will basically wrap your code to execute commands in powershell.exe. Default RunWithString
// runs commands in cmd.exe
//
// Deprecated: use RunPSWithContextWithString()
func (c *Client) RunPSWithString(command string, stdin string) (string, string, int, error) {
	return c.RunPSWithContextWithString(context.Background(), command, stdin)
}

// RunPSWithContextWithString will basically wrap your code to execute commands in powershell.exe. Default RunWithString
// runs commands in cmd.exe
func (c *Client) RunPSWithContextWithString(ctx context.Context, command string, stdin string) (string, string, int, error) {
	command = Powershell(command)

	// Let's check if we actually created a command
	if command == "" {
		return "", "", 1, errors.New("cannot encode the given command")
	}

	// Specify powershell.exe to run encoded command
	return c.RunWithContextWithString(ctx, command, stdin)
}

// RunPSWithContext will basically wrap your code to execute commands in powershell.exe.
// runs commands in cmd.exe
func (c *Client) RunPSWithContext(ctx context.Context, command string) (string, string, int, error) {
	command = Powershell(command)

	// Let's check if we actually created a command
	if command == "" {
		return "", "", 1, errors.New("cannot encode the given command")
	}

	var outWriter, errWriter bytes.Buffer
	exitCode, err := c.RunWithContextWithInput(ctx, command, &outWriter, &errWriter, nil)
	return outWriter.String(), errWriter.String(), exitCode, err
}

// RunWithInput will run command on the the remote host, writing the process stdout and stderr to
// the given writers, and injecting the process stdin with the stdin reader.
// Warning stdin (not stdout/stderr) are bufferized, which means reading only one byte in stdin will
// send a winrm http packet to the remote host. If stdin is a pipe, it might be better for
// performance reasons to buffer it.
// If stdin is nil, this is equivalent to c.Run()
//
// Deprecated: use RunWithContextWithInput()
func (c *Client) RunWithInput(command string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	return c.RunWithContextWithInput(context.Background(), command, stdout, stderr, stdin)
}

// RunWithContextWithInput will run command on the the remote host, writing the process stdout and stderr to
// the given writers, and injecting the process stdin with the stdin reader.
// If the context is canceled, the command on the remote machine is canceled.
// Warning stdin (not stdout/stderr) are bufferized, which means reading only one byte in stdin will
// send a winrm http packet to the remote host. If stdin is a pipe, it might be better for
// performance reasons to buffer it.
// If stdin is nil, this is equivalent to c.RunWithContext()
func (c *Client) RunWithContextWithInput(ctx context.Context, command string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	shell, err := c.CreateShell()
	if err != nil {
		return 1, err
	}
	defer shell.Close()
	cmd, err := shell.ExecuteWithContext(ctx, command)
	if err != nil {
		return 1, err
	}

	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer func() {
			wg.Done()
		}()
		if stdin == nil {
			return
		}
		defer func() {
			cmd.Stdin.Close()
		}()
		_, _ = io.Copy(cmd.Stdin, stdin)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(stdout, cmd.Stdout)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(stderr, cmd.Stderr)
	}()

	cmd.Wait()
	wg.Wait()
	cmd.Close()

	return cmd.ExitCode(), cmd.err
}
