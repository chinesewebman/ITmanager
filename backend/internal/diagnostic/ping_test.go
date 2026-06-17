package diagnostic

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestValidateHost(t *testing.T) {
	cases := []struct {
		host    string
		wantErr bool
	}{
		{"192.168.1.1", false},
		{"google.com", false},
		{"::1", false},
		{"2001:db8::1", false},
		{"host-name.example.com", false},
		{"", true},
		{".", true},
		{"..", true},
		{"host;rm -rf", true},
		{"host with space", true},
		{"a|b", true},
		{strings.Repeat("a", 256), true},
	}
	for _, c := range cases {
		err := validateHost(c.host)
		if (err != nil) != c.wantErr {
			t.Errorf("validateHost(%q): err=%v, wantErr=%v", c.host, err, c.wantErr)
		}
	}
}

func TestParsePingOutput_MacOS(t *testing.T) {
	raw := `PING 127.0.0.1 (127.0.0.1): 56 data bytes
64 bytes from 127.0.0.1: icmp_seq=0 ttl=64 time=0.081 ms
64 bytes from 127.0.0.1: icmp_seq=1 ttl=64 time=0.328 ms

--- 127.0.0.1 ping statistics ---
2 packets transmitted, 2 packets received, 0.0% packet loss
round-trip min/avg/max/stddev = 0.081/0.205/0.328/0.123 ms`

	res := parsePingOutput(raw, "127.0.0.1", 2)
	if res.Transmitted != 2 {
		t.Errorf("transmitted=%d, want 2", res.Transmitted)
	}
	if res.Received != 2 {
		t.Errorf("received=%d, want 2", res.Received)
	}
	if res.LossPercent != 0 {
		t.Errorf("loss=%v, want 0", res.LossPercent)
	}
	if res.MinMs != 0.081 {
		t.Errorf("min=%v, want 0.081", res.MinMs)
	}
	if res.AvgMs != 0.205 {
		t.Errorf("avg=%v, want 0.205", res.AvgMs)
	}
	if res.MaxMs != 0.328 {
		t.Errorf("max=%v, want 0.328", res.MaxMs)
	}
	if res.StddevMs != 0.123 {
		t.Errorf("stddev=%v, want 0.123", res.StddevMs)
	}
}

func TestParsePingOutput_Loss(t *testing.T) {
	raw := `PING 10.0.0.1 (10.0.0.1): 56 data bytes
Request timeout for icmp_seq 0

--- 10.0.0.1 ping statistics ---
3 packets transmitted, 1 packets received, 66.7% packet loss`

	res := parsePingOutput(raw, "10.0.0.1", 3)
	if res.Transmitted != 3 || res.Received != 1 {
		t.Errorf("transmitted/received=%d/%d, want 3/1", res.Transmitted, res.Received)
	}
	if res.LossPercent != 66.7 {
		t.Errorf("loss=%v, want 66.7", res.LossPercent)
	}
}

func TestPing_InvalidHost(t *testing.T) {
	_, err := Ping(context.Background(), "host;rm -rf", 3)
	if !errors.Is(err, ErrInvalidHost) {
		t.Errorf("err=%v, want ErrInvalidHost", err)
	}
}

func TestPing_InvalidCount(t *testing.T) {
	_, err := Ping(context.Background(), "127.0.0.1", 0)
	if !errors.Is(err, ErrInvalidHost) {
		t.Errorf("err=%v, want ErrInvalidHost", err)
	}
	_, err = Ping(context.Background(), "127.0.0.1", 100)
	if !errors.Is(err, ErrInvalidHost) {
		t.Errorf("err=%v, want ErrInvalidHost", err)
	}
}

func TestPing_Live(t *testing.T) {
	// 仅在 ping binary 可用时跑
	if _, err := exec.LookPath("ping"); err != nil {
		t.Skip("ping binary not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := Ping(ctx, "127.0.0.1", 2)
	if err != nil {
		t.Fatalf("Ping live: %v", err)
	}
	if res.Received < 1 {
		t.Errorf("expected ≥1 received, got %d", res.Received)
	}
	if res.AvgMs <= 0 {
		t.Errorf("expected avg>0, got %v", res.AvgMs)
	}
}

func TestTraceroute_InvalidHost(t *testing.T) {
	_, err := Traceroute(context.Background(), "bad host", 5)
	if !errors.Is(err, ErrInvalidHost) {
		t.Errorf("err=%v, want ErrInvalidHost", err)
	}
}

func TestTraceroute_InvalidHops(t *testing.T) {
	_, err := Traceroute(context.Background(), "127.0.0.1", 0)
	if !errors.Is(err, ErrInvalidHost) {
		t.Errorf("err=%v, want ErrInvalidHost", err)
	}
	_, err = Traceroute(context.Background(), "127.0.0.1", 100)
	if !errors.Is(err, ErrInvalidHost) {
		t.Errorf("err=%v, want ErrInvalidHost", err)
	}
}

func TestParseTracerouteOutput_Basic(t *testing.T) {
	raw := `traceroute to 127.0.0.1 (127.0.0.1), 5 hops max, 40 byte packets
 1  localhost (127.0.0.1)  1.773 ms  0.877 ms  0.127 ms
 2  10.0.0.1  5.123 ms  4.987 ms  5.045 ms
 3  * * *
 5  example.com (10.0.0.2)  10.0 ms  11.0 ms  12.0 ms`

	res := parseTracerouteOutput(raw, "10.0.0.2", 5)
	if len(res.Hops) != 4 {
		t.Fatalf("hops=%d, want 4", len(res.Hops))
	}
	if res.Hops[0].Host != "localhost" {
		t.Errorf("hop1 host=%q, want localhost", res.Hops[0].Host)
	}
	if res.Hops[0].IP != "127.0.0.1" {
		t.Errorf("hop1 ip=%q, want 127.0.0.1", res.Hops[0].IP)
	}
	if res.Hops[2].Lossed != true {
		t.Errorf("hop3 should be lossed (* * *)")
	}
	if !res.Reached {
		t.Errorf("expected reached=true")
	}
}

func TestParseHopLine_Stars(t *testing.T) {
	h := parseHopLine("* * *")
	if !h.Lossed {
		t.Errorf("expected lossed=true for * * *")
	}
}

func TestParseHopLine_Plain(t *testing.T) {
	h := parseHopLine("10.0.0.1  5.123 ms  4.987 ms  5.045 ms")
	if h.Host != "10.0.0.1" || h.IP != "10.0.0.1" {
		t.Errorf("got host=%q ip=%q", h.Host, h.IP)
	}
	if len(h.RTTs) != 3 {
		t.Errorf("rtts=%d, want 3", len(h.RTTs))
	}
}

func TestParseHopLine_Paren(t *testing.T) {
	h := parseHopLine("router.example.com (10.0.0.1)  5.0 ms  4.0 ms  3.0 ms")
	if h.Host != "router.example.com" || h.IP != "10.0.0.1" {
		t.Errorf("got host=%q ip=%q", h.Host, h.IP)
	}
}
