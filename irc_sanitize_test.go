package irc

import (
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"hello\r\nQUIT", "helloQUIT"},
		{"hello\nQUIT", "helloQUIT"},
		{"hello\rQUIT", "helloQUIT"},
		{"hello\x00QUIT", "helloQUIT"},
		{"keep spaces and :colons", "keep spaces and :colons"},
	}
	for _, c := range cases {
		if got := sanitize(c.in); got != c.want {
			t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeParam(t *testing.T) {
	cases := []struct{ in, want string }{
		{"#channel", "#channel"},
		{"#a,#b", "#a,#b"},
		{"#chan\r\nQUIT", "#chanQUIT"},
		{"#a #b", "#a#b"},
		{":trailing", "trailing"},
		{"::trailing", "trailing"},
		{"mid:colon", "mid:colon"},
		{" :#chan", "#chan"},
	}
	for _, c := range cases {
		if got := sanitizeParam(c.in); got != c.want {
			t.Errorf("sanitizeParam(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Every send path must emit exactly one line, whatever the caller passes in.
func TestSendPathsRejectCRLFInjection(t *testing.T) {
	const evil = "abc\r\nQUIT :owned\r\n"

	sends := map[string]func(c *Connection){
		"Join":      func(c *Connection) { c.Join(evil) },
		"Part":      func(c *Connection) { c.Part(evil) },
		"Notice":    func(c *Connection) { c.Notice(evil, evil) },
		"Noticef":   func(c *Connection) { c.Noticef(evil, "%s", evil) },
		"Action":    func(c *Connection) { c.Action(evil, evil) },
		"Actionf":   func(c *Connection) { c.Actionf(evil, "%s", evil) },
		"Privmsg":   func(c *Connection) { c.Privmsg(evil, evil) },
		"Privmsgf":  func(c *Connection) { c.Privmsgf(evil, "%s", evil) },
		"Kick":      func(c *Connection) { c.Kick(evil, evil, evil) },
		"MultiKick": func(c *Connection) { c.MultiKick([]string{evil, evil}, evil, evil) },
		"SendRaw":   func(c *Connection) { c.SendRaw("PRIVMSG #x :" + evil) },
		"SendRawf":  func(c *Connection) { c.SendRawf("PRIVMSG #x :%s", evil) },
		"Nick":      func(c *Connection) { c.Nick(evil) },
		"Whois":     func(c *Connection) { c.Whois(evil) },
		"Who":       func(c *Connection) { c.Who(evil) },
		"Mode":      func(c *Connection) { c.Mode(evil, evil, evil) },
		"Quit":      func(c *Connection) { c.QuitMessage = evil; c.Quit() },
	}

	for name, send := range sends {
		t.Run(name, func(t *testing.T) {
			c := IRC("nick", "user")
			c.pwrite = make(chan string, 10)
			send(c)

			select {
			case line := <-c.pwrite:
				if n := strings.Count(line, "\r\n"); n != 1 {
					t.Errorf("emitted %d lines, want 1: %q", n, line)
				}
				if !strings.HasSuffix(line, "\r\n") {
					t.Errorf("line does not end in CRLF: %q", line)
				}
				if strings.Contains(line, "\x00") {
					t.Errorf("line contains NUL: %q", line)
				}
				if body := strings.TrimSuffix(line, "\r\n"); strings.ContainsAny(body, "\r\n") {
					t.Errorf("bare CR/LF left inside the line: %q", line)
				}
			default:
				t.Fatal("nothing was sent")
			}
		})
	}
}

func TestRegistrationLinesSanitized(t *testing.T) {
	c := IRC("nick\r\nQUIT", "user\r\nQUIT")
	c.Password = "pass\r\nQUIT"
	c.WebIRC = "web\r\nQUIT"
	c.RealName = "real\r\nQUIT"
	c.pwrite = make(chan string, 10)

	// Mirrors the writes Connect performs after the socket is up.
	c.pwrite <- "WEBIRC " + sanitize(c.WebIRC) + "\r\n"
	c.pwrite <- "PASS " + sanitizeParam(c.Password) + "\r\n"
	c.pwrite <- "NICK " + sanitizeParam(c.nick) + "\r\n"
	c.pwrite <- "USER " + sanitizeParam(c.user) + " 0.0.0.0 0.0.0.0 :" + sanitize(c.RealName) + "\r\n"
	close(c.pwrite)

	for line := range c.pwrite {
		if n := strings.Count(line, "\r\n"); n != 1 {
			t.Errorf("registration line has %d line endings: %q", n, line)
		}
	}
}

func TestJoinKeepsPassword(t *testing.T) {
	c := IRC("nick", "user")
	c.pwrite = make(chan string, 10)
	c.Join("#channel secretkey")
	if got := <-c.pwrite; got != "JOIN #channel secretkey\r\n" {
		t.Errorf("Join mangled the channel key: %q", got)
	}
}

func TestModeKeepsArguments(t *testing.T) {
	c := IRC("nick", "user")
	c.pwrite = make(chan string, 10)
	c.Mode("#channel", "+o", "someone")
	if got := <-c.pwrite; got != "MODE #channel +o someone\r\n" {
		t.Errorf("Mode mangled its arguments: %q", got)
	}
}
