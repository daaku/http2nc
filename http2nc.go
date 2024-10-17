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

var dialer net.Dialer

func DialConnect(w http.ResponseWriter, r *http.Request, addr string) error {
	defer r.Body.Close()
	wr := newWriter(w)
	if !r.ProtoAtLeast(2, 0) {
		if err := wr.rc.EnableFullDuplex(); err != nil {
			return fmt.Errorf("http2nc: enabling full-duplex: %w", err)
		}
	}
	nc, err := dialer.DialContext(r.Context(), "tcp", addr)
	if err != nil {
		return fmt.Errorf("http2nc: dialing: %w", err)
	}
	tcpc := nc.(*net.TCPConn)
	defer tcpc.Close()

	var eg errgroup.Group
	eg.Go(func() error {
		if _, err := io.Copy(tcpc, r.Body); err != nil {
			tcpc.Close()
			return fmt.Errorf("http2nc: copying data to net.Conn from http: %w", err)
		}
		if err := tcpc.CloseWrite(); err != nil {
			tcpc.Close()
			return fmt.Errorf("http2nc: CloseWrite of net.Conn: %w", err)
		}
		return nil
	})
	eg.Go(func() error {
		if _, err := io.Copy(wr, tcpc); err != nil {
			tcpc.Close()
			return fmt.Errorf("http2nc: copying data to http from net.Conn: %w", err)
		}
		if err := tcpc.CloseRead(); err != nil {
			tcpc.Close()
			return fmt.Errorf("http2nc: CloseRead of net.Conn: %w", err)
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
