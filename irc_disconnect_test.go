package irc

import (
	"bufio"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"
)

type fakeServer struct {
	listener net.Listener
	accepted chan net.Conn
	lines    chan string
}

func startFakeServer(t *testing.T) *fakeServer {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %s", err)
	}
	t.Cleanup(func() { listener.Close() })

	server := &fakeServer{
		listener: listener,
		accepted: make(chan net.Conn, 1),
		lines:    make(chan string, 64),
	}

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		server.accepted <- conn

		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			server.lines <- strings.TrimRight(line, "\r\n")
		}
	}()

	return server
}

func (server *fakeServer) hangUp(t *testing.T) {
	t.Helper()
	select {
	case conn := <-server.accepted:
		conn.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted a connection")
	}
}

func (server *fakeServer) nextLine(t *testing.T) string {
	t.Helper()
	select {
	case line := <-server.lines:
		return line
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a line from the client")
	}
	return ""
}

func connectToFakeServer(t *testing.T) (*Connection, *fakeServer) {
	t.Helper()

	server := startFakeServer(t)

	irc := IRC("gotest", "gotest")
	irc.Log = log.New(io.Discard, "", 0)
	irc.Timeout = 5 * time.Second
	irc.PingFreq = 1 * time.Hour
	irc.KeepAlive = 1 * time.Hour

	if err := irc.Connect(server.listener.Addr().String()); err != nil {
		t.Fatalf("connect: %s", err)
	}
	return irc, server
}

func TestSendWhileConnected(t *testing.T) {
	irc, server := connectToFakeServer(t)

	if got := server.nextLine(t); got != "NICK gotest" {
		t.Errorf("first line was %q, want %q", got, "NICK gotest")
	}
	if got := server.nextLine(t); !strings.HasPrefix(got, "USER gotest ") {
		t.Errorf("second line was %q, want a USER line", got)
	}

	irc.Privmsg("#chan", "hello")
	if got := server.nextLine(t); got != "PRIVMSG #chan :hello" {
		t.Errorf("sent %q, want %q", got, "PRIVMSG #chan :hello")
	}

	server.hangUp(t)
	irc.Disconnect()
}

func TestSendAfterDisconnect(t *testing.T) {
	irc, server := connectToFakeServer(t)

	server.hangUp(t)
	irc.Disconnect()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			irc.Privmsg("#chan", "hello")
			irc.Notice("#chan", "hello")
			irc.Action("#chan", "waves")
			irc.Join("#chan")
			irc.Part("#chan")
			irc.Kick("someone", "#chan", "bye")
			irc.MultiKick([]string{"a", "b"}, "#chan", "bye")
			irc.SendRaw("PING :x")
			irc.Mode("#chan", "+o", "someone")
			irc.Whois("someone")
			irc.Who("someone")
			irc.Nick("gotest2")
		}
		irc.Quit()
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("a send after Disconnect blocked")
	}
}

func TestSendReportsDisconnection(t *testing.T) {
	irc, server := connectToFakeServer(t)

	if err := irc.send("PING :x\r\n"); err != nil {
		t.Errorf("send while connected returned %v, want nil", err)
	}

	server.hangUp(t)
	irc.Disconnect()

	if err := irc.send("PING :x\r\n"); err != ErrDisconnected {
		t.Errorf("send after Disconnect returned %v, want %v", err, ErrDisconnected)
	}
}
