package metrics

import (
	"strings"
	"sync"
	"testing"
)

func TestCounterVec_IncAndAdd(t *testing.T) {
	r := New()
	c := r.NewCounterVec("test_total", "测试", []string{"method", "status"})

	c.Inc("GET", "200")
	c.Inc("GET", "200")
	c.Add(5, "GET", "500")

	var sb strings.Builder
	if err := r.WriteAll(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	// 验证两条 series + HELP/TYPE 行
	if !strings.Contains(out, `test_total{method="GET",status="200"} 2`) {
		t.Errorf("missing 200 counter, got: %s", out)
	}
	if !strings.Contains(out, `test_total{method="GET",status="500"} 5`) {
		t.Errorf("missing 500 counter, got: %s", out)
	}
	if !strings.Contains(out, "# TYPE test_total counter") {
		t.Errorf("missing TYPE line")
	}
}

func TestGaugeVec_Set(t *testing.T) {
	r := New()
	g := r.NewGaugeVec("gauge_test", "测试 gauge", []string{"pool"})

	g.Set(10, "open")
	g.Set(7, "in_use")
	g.Set(3, "idle")

	if g.GetRaw("open") != 10 {
		t.Errorf("expected 10, got %v", g.GetRaw("open"))
	}
	if g.GetRaw("in_use") != 7 {
		t.Errorf("expected 7, got %v", g.GetRaw("in_use"))
	}
}

func TestHistogramVec_Observe(t *testing.T) {
	r := New()
	h := r.NewHistogramVec("lat", "延迟", []string{"op"}, []float64{0.1, 0.5, 1.0})

	// 5 个观测：0.05, 0.2, 0.2, 0.7, 1.5
	h.Observe(0.05, "do")
	h.Observe(0.2, "do")
	h.Observe(0.2, "do")
	h.Observe(0.7, "do")
	h.Observe(1.5, "do")

	var sb strings.Builder
	if err := r.WriteAll(&sb); err != nil {
		t.Fatal(err)
	}
	out := sb.String()
	// count = 5
	if !strings.Contains(out, "lat_count{op=\"do\"} 5") {
		t.Errorf("count wrong, got: %s", out)
	}
	// bucket 0.1 = 1 (0.05)
	if !strings.Contains(out, `lat_bucket{op="do",le="0.1"} 1`) {
		t.Errorf("bucket 0.1 wrong, got: %s", out)
	}
	// bucket 0.5 = 3 (0.05+0.2+0.2)
	if !strings.Contains(out, `lat_bucket{op="do",le="0.5"} 3`) {
		t.Errorf("bucket 0.5 wrong, got: %s", out)
	}
	// bucket 1.0 = 4 (累计到 0.7)
	if !strings.Contains(out, `lat_bucket{op="do",le="1"} 4`) {
		t.Errorf("bucket 1.0 wrong, got: %s", out)
	}
	// +Inf = 5
	if !strings.Contains(out, `lat_bucket{op="do",le="+Inf"} 5`) {
		t.Errorf("+Inf bucket wrong, got: %s", out)
	}
}

func TestRegistry_Concurrent(t *testing.T) {
	// 简单并发压力，验证无 race
	r := New()
	c := r.NewCounterVec("conc", "并发", []string{"k"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				c.Inc("a")
			}
		}()
	}
	wg.Wait()

	var sb strings.Builder
	_ = r.WriteAll(&sb)
	if !strings.Contains(sb.String(), "conc{k=\"a\"} 10000") {
		t.Errorf("expected 10000, got: %s", sb.String())
	}
}

func TestRegistry_Forwarders(t *testing.T) {
	r := New()
	r.NewCounterVec("fwd_c", "c", []string{"a"})
	r.NewGaugeVec("fwd_g", "g", nil)
	r.NewHistogramVec("fwd_h", "h", nil, []float64{1, 2})

	r.IncCounter("fwd_c", "x")
	r.AddCounter("fwd_c", 4, "x")
	r.SetGauge("fwd_g", 42)
	r.ObserveHistogram("fwd_h", 1.5)

	var sb strings.Builder
	_ = r.WriteAll(&sb)
	out := sb.String()
	if !strings.Contains(out, `fwd_c{a="x"} 5`) {
		t.Errorf("counter wrong: %s", out)
	}
	if !strings.Contains(out, "fwd_g 42") {
		t.Errorf("gauge wrong: %s", out)
	}
	if !strings.Contains(out, "fwd_h_count 1") {
		t.Errorf("histogram wrong: %s", out)
	}

	// 未知 name 走 noop，不 panic
	r.IncCounter("nope", "x")
	r.SetGauge("nope", 1)
	r.ObserveHistogram("nope", 1)
}

func TestFormatLabels_Escape(t *testing.T) {
	got := formatLabels([]string{"k"}, []string{`v"with"quote`})
	want := `{k="v\"with\"quote"}`
	if got != want {
		t.Errorf("escape wrong: %s", got)
	}
}

func TestFormatLE(t *testing.T) {
	// 桶上界用 %g 格式，0.1 → "0.1", 1.0 → "1"
	if got := formatLE(0.1); got != "0.1" {
		t.Errorf("formatLE(0.1) = %s", got)
	}
	if got := formatLE(1.0); got != "1" {
		t.Errorf("formatLE(1.0) = %s", got)
	}
	if got := formatLE(0.005); got != "0.005" {
		t.Errorf("formatLE(0.005) = %s", got)
	}
}
