package http2nc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"syscall"

	"golang.org/x/sync/errgroup"
)

type writer struct {
	rc *http.ResponseController
	w  http.ResponseWriter
}

func newWriter(w http.ResponseWriter) *writer {
	return &writer{
		rc: http.NewResponseController(w),
		w:  w,
	}
}

func (w *writer) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	if err == nil {
		err = w.rc.Flush()
	}
	return n, err
}

func DialConnect(w http.ResponseWriter, r *http.Request, addr string) error {
	if !r.ProtoAtLeast(2, 0) {
		return errors.New("ssh: must connect using HTTP/2 or higher")
	}
	wr := newWriter(w)
	sshConnC, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("ssh: %w", err)
	}
	sshConn := sshConnC.(*net.TCPConn)
	defer sshConn.Close() // backup close

	var eg errgroup.Group
	eg.Go(func() error {
		if _, err := io.Copy(sshConn, r.Body); err != nil {
			sshConn.Close()
			return fmt.Errorf("ssh: copying data to ssh from http: %w", err)
		}
		if err := sshConn.CloseWrite(); err != nil {
			sshConn.Close()
			return fmt.Errorf("ssh: CloseWrite of ssh: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		if _, err := io.Copy(wr, sshConn); err != nil {
			sshConn.Close()
			return fmt.Errorf("ssh: copying data to http from ssh: %w", err)
		}
		if err := sshConn.CloseRead(); err != nil {
			sshConn.Close()
			return fmt.Errorf("ssh: CloseRead of ssh: %w", err)
		}
		return nil
	})
	err = eg.Wait()
	if err == nil ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ENOTCONN) {
		return nil
	}
	return err
}
