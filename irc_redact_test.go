package irc

import (
	"bufio"
	"bytes"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/text/encoding"
)

func TestRedactLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{"PASS hunter2\r\n", "PASS <redacted>"},
		{"PASS user/network:hunter2", "PASS <redacted>"},
		{"pass hunter2", "pass <redacted>"},
		{"AUTHENTICATE aGVucnkAaGVucnkAaHVudGVyMg==", "AUTHENTICATE <redacted>"},
		{"OPER admin hunter2", "OPER admin <redacted>"},
		{"OPER hunter2", "OPER <redacted>"},

		{"AUTHENTICATE PLAIN", "AUTHENTICATE PLAIN"},
		{"AUTHENTICATE EXTERNAL", "AUTHENTICATE EXTERNAL"},
		{"AUTHENTICATE +", "AUTHENTICATE +"},
		{"AUTHENTICATE *", "AUTHENTICATE *"},

		{"NICK nick", "NICK nick"},
		{"PRIVMSG #chan :hello", "PRIVMSG #chan :hello"},
		{"JOIN #chan key", "JOIN #chan key"},
		{"CAP END", "CAP END"},
		{"PASS", "PASS"},
		{"", ""},
	}
	for _, c := range cases {
		if got := redactLine(c.in); got != c.want {
			t.Errorf("redactLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWriteLoopLogsRedactedButSendsRealCredentials(t *testing.T) {
	const line = "PASS hunter2\r\n"

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	logged := &bytes.Buffer{}
	c := IRC("nick", "user")
	c.Debug = true
	c.Log = log.New(logged, "", 0)
	c.socket = client
	c.Encoding = encoding.Nop // Connect does this; writeLoop needs it.
	c.pwrite = make(chan string, 1)
	c.end = make(chan struct{})
	c.Error = make(chan error, 1)

	c.Add(1)
	go c.writeLoop()
	defer close(c.end)

	c.pwrite <- line

	server.SetReadDeadline(time.Now().Add(5 * time.Second))
	got, err := bufio.NewReader(server).ReadString('\n')
	if err != nil {
		t.Fatalf("reading from the socket: %s", err)
	}
	if got != line {
		t.Errorf("wrote %q to the socket, want %q", got, line)
	}

	if out := logged.String(); strings.Contains(out, "hunter2") {
		t.Errorf("password leaked into the log: %q", out)
	} else if !strings.Contains(out, "--> PASS <redacted>") {
		t.Errorf("log line = %q, want it to contain %q", out, "--> PASS <redacted>")
	}
}

func TestSASLPayloadRedactedInLog(t *testing.T) {
	c := IRC("nick", "user")
	c.SASLLogin = "henry"
	c.SASLPassword = "hunter2"
	c.pwrite = make(chan string, 10)

	result := make(chan *SASLResult, 1)
	c.setupSASLCallbacks(result)
	c.RunCallbacks(&Event{Code: "AUTHENTICATE", Connection: c})

	select {
	case line := <-c.pwrite:
		if !strings.HasPrefix(line, "AUTHENTICATE ") || strings.TrimSpace(line) == "AUTHENTICATE" {
			t.Fatalf("unexpected SASL line: %q", line)
		}
		if got := redactLine(line); got != "AUTHENTICATE <redacted>" {
			t.Errorf("redactLine(%q) = %q, want %q", line, got, "AUTHENTICATE <redacted>")
		}
	default:
		t.Fatal("no SASL response was sent")
	}
}
