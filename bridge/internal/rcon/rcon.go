package rcon

import (
	"sync"

	gorcon "github.com/gorcon/rcon"
)

// Client is a thin, reconnecting wrapper around a Factorio RCON connection.
type Client struct {
	addr     string
	password string

	mu   sync.Mutex
	conn *gorcon.Conn
}

func New(addr, password string) *Client {
	return &Client{addr: addr, password: password}
}

// Execute runs a command, dialing on first use and retrying once if the connection
// has dropped (game restart, idle timeout).
func (c *Client) Execute(cmd string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		if err := c.dial(); err != nil {
			return "", err
		}
	}

	resp, err := c.conn.Execute(cmd)
	if err != nil {
		c.conn.Close()
		c.conn = nil
		if derr := c.dial(); derr != nil {
			return "", derr
		}
		resp, err = c.conn.Execute(cmd)
	}
	return resp, err
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *Client) dial() error {
	conn, err := gorcon.Dial(c.addr, c.password)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}
