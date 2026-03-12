package proxy

import (
	"io"
	"sync/atomic"
)

type countingReadCloser struct {
	io.ReadCloser
	count *atomic.Int64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 {
		c.count.Add(int64(n))
	}
	return n, err
}
