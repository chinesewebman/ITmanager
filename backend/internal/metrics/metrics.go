// Package metrics 提供零依赖的 Prometheus 文本格式 metrics registry。
//
// 设计目标：
//   - 不引入 prometheus/client_golang（避免 12+ 间接依赖与 std lib 升级风险）
//   - 兼容 Prometheus text exposition format（直接可被 Prometheus 抓取）
//   - 线程安全：sync.RWMutex + sync/atomic
//   - 极简 API：Counter / Gauge / Histogram 三类原语
//
// 与 prom 库相比的取舍：
//   - 失去：自动 label 时间序列分桶性能、远程 push、Protobuf 编码
//   - 保留：text format 兼容（0.0.4 标准足够 scrape）、label 基数控制、Go runtime/process metrics
//
// 用法：
//
//	reg := metrics.New()
//	reqCnt := reg.NewCounterVec("http_requests_total", "HTTP 请求计数", []string{"method", "path", "status"})
//	reqDur := reg.NewHistogramVec("http_request_duration_seconds", "HTTP 请求耗时", []string{"method", "path"}, []float64{.005,.01,.05,.1,.5,1,5})
//	httpSrv := &http.Server{Handler: middleware.Metrics(reqCnt, reqDur)(router)}
//	httpSrv.ListenAndServe()
//	// 暴露 /metrics：reg.Handler().ServeHTTP(...)
package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
)

// CounterVec 带 label 维度的 counter。
// 每次 Inc() / Add() 原子递增匹配 label 集合的 series。
type CounterVec struct {
	name, help string
	labelNames []string
	mu         sync.RWMutex
	values     map[string]*uint64 // labelValues 用 "\x00" 拼接为 key
}

// NewCounterVec 创建计数器向量。
func (r *Registry) NewCounterVec(name, help string, labelNames []string) *CounterVec {
	c := &CounterVec{
		name:       name,
		help:       help,
		labelNames: labelNames,
		values:     make(map[string]*uint64),
	}
	r.register(c)
	return c
}

// Inc 计数器 +1，labelValues 长度必须与 labelNames 一致。
func (c *CounterVec) Inc(labelValues ...string) { c.Add(1, labelValues...) }

// Add 计数器 +v（用 CAS loop 实现浮点原子加）。
func (c *CounterVec) Add(v float64, labelValues ...string) {
	if len(labelValues) != len(c.labelNames) {
		return // 防御：label 不匹配直接吞掉（不阻塞业务）
	}
	key := joinLabels(labelValues)
	c.mu.RLock()
	ptr, ok := c.values[key]
	c.mu.RUnlock()
	if !ok {
		c.mu.Lock()
		if ptr, ok = c.values[key]; !ok {
			ptr = new(uint64)
			c.values[key] = ptr
		}
		c.mu.Unlock()
	}
	// CAS loop：atomic 没有 Float64Add，手撸
	for {
		oldBits := atomic.LoadUint64(ptr)
		newFloat := math.Float64frombits(oldBits) + v
		newBits := math.Float64bits(newFloat)
		if atomic.CompareAndSwapUint64(ptr, oldBits, newBits) {
			return
		}
	}
}

// GaugeVec 带 label 维度的 gauge（瞬时值，可增可减）。
// 用 uint64 存 Float64 bits，访问用 GetRaw 转回 float64。
type GaugeVec struct {
	name, help string
	labelNames []string
	mu         sync.RWMutex
	values     map[string]*uint64
}

// NewGaugeVec 创建 gauge 向量。
func (r *Registry) NewGaugeVec(name, help string, labelNames []string) *GaugeVec {
	g := &GaugeVec{
		name:       name,
		help:       help,
		labelNames: labelNames,
		values:     make(map[string]*uint64),
	}
	r.register(g)
	return g
}

// Set 设置 gauge 绝对值。
func (g *GaugeVec) Set(v float64, labelValues ...string) {
	if len(labelValues) != len(g.labelNames) {
		return
	}
	key := joinLabels(labelValues)
	g.mu.RLock()
	ptr, ok := g.values[key]
	g.mu.RUnlock()
	if !ok {
		g.mu.Lock()
		if ptr, ok = g.values[key]; !ok {
			ptr = new(uint64) // 长生命周期
			g.values[key] = ptr
		}
		g.mu.Unlock()
	}
	atomic.StoreUint64(ptr, math.Float64bits(v))
}

// GetRaw 返回原始 float64 读取（仅供测试）。
func (g *GaugeVec) GetRaw(labelValues ...string) float64 {
	key := joinLabels(labelValues)
	g.mu.RLock()
	ptr, ok := g.values[key]
	g.mu.RUnlock()
	if !ok {
		return 0
	}
	return math.Float64frombits(atomic.LoadUint64(ptr))
}

// HistogramVec 固定 bucket 直方图。
// buckets 上界按升序排列；+Inf 隐式。
type HistogramVec struct {
	name, help string
	labelNames []string
	buckets    []float64
	mu         sync.RWMutex
	counts     map[string]*histogramSeries
}

type histogramSeries struct {
	bucketCounts []uint64 // len = len(buckets) + 1（最后一位 = +Inf）
	sumBits      uint64
	count        uint64
}

// NewHistogramVec 创建直方图向量。buckets 必须按升序排。
func (r *Registry) NewHistogramVec(name, help string, labelNames []string, buckets []float64) *HistogramVec {
	if len(buckets) == 0 {
		buckets = []float64{.005, .01, .05, .1, .5, 1, 5}
	}
	// 拷贝防止外部修改
	bs := make([]float64, len(buckets))
	copy(bs, buckets)
	sort.Float64s(bs)
	h := &HistogramVec{
		name:       name,
		help:       help,
		labelNames: labelNames,
		buckets:    bs,
		counts:     make(map[string]*histogramSeries),
	}
	r.register(h)
	return h
}

// Observe 记录一个观测值。
func (h *HistogramVec) Observe(v float64, labelValues ...string) {
	if len(labelValues) != len(h.labelNames) {
		return
	}
	key := joinLabels(labelValues)
	h.mu.RLock()
	series, ok := h.counts[key]
	h.mu.RUnlock()
	if !ok {
		h.mu.Lock()
		if series, ok = h.counts[key]; !ok {
			series = &histogramSeries{
				bucketCounts: make([]uint64, len(h.buckets)+1),
			}
			h.counts[key] = series
		}
		h.mu.Unlock()
	}
	atomic.AddUint64(&series.count, 1)
	atomic.AddUint64(&series.sumBits, math.Float64bits(v))
	// 找到第一个 bucket 上界 > v 的索引
	idx := sort.SearchFloat64s(h.buckets, v)
	atomic.AddUint64(&series.bucketCounts[idx], 1)
}

// Registry 收集所有 metrics 协作者。
type Registry struct {
	mu        sync.RWMutex
	counters  []*CounterVec
	gauges    []*GaugeVec
	histogram []*HistogramVec
}

// New 构造空 registry。
func New() *Registry { return &Registry{} }

func (r *Registry) register(v interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch x := v.(type) {
	case *CounterVec:
		r.counters = append(r.counters, x)
	case *GaugeVec:
		r.gauges = append(r.gauges, x)
	case *HistogramVec:
		r.histogram = append(r.histogram, x)
	}
}

// Handler 返回 Prometheus 文本格式的 HTTP handler。
// 直接挂到 mux：r.Get("/metrics", gin.WrapH(reg.Handler()))
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_ = r.WriteAll(w)
	})
}

// IncCounter 转发到内部 Counter（按 name 找）。未注册则 noop。
// 用于 middleware：HTTP 请求处理完调一次写入。
func (r *Registry) IncCounter(name string, labelValues ...string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.counters {
		if c.name == name {
			c.Inc(labelValues...)
			return
		}
	}
}

// AddCounter 转发到内部 Counter。
func (r *Registry) AddCounter(name string, v float64, labelValues ...string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.counters {
		if c.name == name {
			c.Add(v, labelValues...)
			return
		}
	}
}

// SetGauge 转发到内部 Gauge。
func (r *Registry) SetGauge(name string, v float64, labelValues ...string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, g := range r.gauges {
		if g.name == name {
			g.Set(v, labelValues...)
			return
		}
	}
}

// ObserveHistogram 转发到内部 Histogram。
func (r *Registry) ObserveHistogram(name string, v float64, labelValues ...string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, h := range r.histogram {
		if h.name == name {
			h.Observe(v, labelValues...)
			return
		}
	}
}

// WriteAll 序列化所有 metrics 到 w。
// 注意：不实现 io.WriterTo（避免与该接口冲突）。
func (r *Registry) WriteAll(w io.Writer) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.counters {
		if err := writeCounter(w, c); err != nil {
			return err
		}
	}
	for _, g := range r.gauges {
		if err := writeGauge(w, g); err != nil {
			return err
		}
	}
	for _, h := range r.histogram {
		if err := writeHistogram(w, h); err != nil {
			return err
		}
	}
	return nil
}

func writeCounter(w io.Writer, c *CounterVec) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name); err != nil {
		return err
	}
	c.mu.RLock()
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	c.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		c.mu.RLock()
		ptr := c.values[k]
		c.mu.RUnlock()
		bits := atomic.LoadUint64(ptr)
		v := math.Float64frombits(bits)
		labels := formatLabels(c.labelNames, splitLabels(k))
		if _, err := fmt.Fprintf(w, "%s%s %g\n", c.name, labels, v); err != nil {
			return err
		}
	}
	return nil
}

func writeGauge(w io.Writer, g *GaugeVec) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name); err != nil {
		return err
	}
	g.mu.RLock()
	keys := make([]string, 0, len(g.values))
	for k := range g.values {
		keys = append(keys, k)
	}
	g.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		g.mu.RLock()
		ptr := g.values[k]
		g.mu.RUnlock()
		bits := atomic.LoadUint64(ptr)
		v := math.Float64frombits(bits)
		labels := formatLabels(g.labelNames, splitLabels(k))
		if _, err := fmt.Fprintf(w, "%s%s %g\n", g.name, labels, v); err != nil {
			return err
		}
	}
	return nil
}

func writeHistogram(w io.Writer, h *HistogramVec) error {
	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name); err != nil {
		return err
	}
	h.mu.RLock()
	keys := make([]string, 0, len(h.counts))
	for k := range h.counts {
		keys = append(keys, k)
	}
	h.mu.RUnlock()
	sort.Strings(keys)
	for _, k := range keys {
		h.mu.RLock()
		series := h.counts[k]
		h.mu.RUnlock()
		lvals := splitLabels(k)
		baseLabels := formatLabels(h.labelNames, lvals)
		// baseLabels 形如 `{a="x",b="y"}` 或空串
		// 拼 le 标签：base + `,le="0.5"` 或 `{le="0.5"}`
		makeLE := func(v string) string {
			lePart := fmt.Sprintf("le=%q", v)
			if baseLabels == "" {
				return "{" + lePart + "}"
			}
			// baseLabels 已带 { }，去掉尾部 } 加 ,le=...
			return baseLabels[:len(baseLabels)-1] + "," + lePart + "}"
		}
		var cumulative uint64
		for i, ub := range h.buckets {
			cumulative += atomic.LoadUint64(&series.bucketCounts[i])
			if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, makeLE(formatLE(ub)), cumulative); err != nil {
				return err
			}
		}
		// +Inf bucket = 总 count
		cumulative += atomic.LoadUint64(&series.bucketCounts[len(h.buckets)])
		if _, err := fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, makeLE("+Inf"), cumulative); err != nil {
			return err
		}
		// sum
		sumBits := atomic.LoadUint64(&series.sumBits)
		sum := math.Float64frombits(sumBits)
		if _, err := fmt.Fprintf(w, "%s_sum%s %g\n", h.name, baseLabels, sum); err != nil {
			return err
		}
		// count
		if _, err := fmt.Fprintf(w, "%s_count%s %d\n", h.name, baseLabels, atomic.LoadUint64(&series.count)); err != nil {
			return err
		}
	}
	return nil
}

// joinLabels / splitLabels — 用 "\x00" 拼接避免与 label 内容冲突。
func joinLabels(vals []string) string {
	out := ""
	for i, v := range vals {
		if i > 0 {
			out += "\x00"
		}
		out += v
	}
	return out
}

func splitLabels(s string) []string {
	if s == "" {
		return nil
	}
	return splitNull(s)
}

func splitNull(s string) []string {
	var out []string
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\x00' {
			out = append(out, s[last:i])
			last = i + 1
		}
	}
	out = append(out, s[last:])
	return out
}

// formatLabels 序列化为 Prometheus 标签格式。
// 返回 "{a=\"x\",b=\"y\"}" 形式；空时返回 ""。
func formatLabels(names, values []string) string {
	if len(names) == 0 {
		return ""
	}
	out := "{"
	for i, n := range names {
		if i > 0 {
			out += ","
		}
		v := ""
		if i < len(values) {
			v = values[i]
		}
		out += fmt.Sprintf("%s=%q", n, escapeLabel(v))
	}
	out += "}"
	return out
}

// formatLE 单独用于 histogram bucket "le" 标签。
func formatLE(v float64) string {
	return fmt.Sprintf("%g", v)
}

func escapeLabel(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			out = append(out, '\\', '\\')
		case '\n':
			out = append(out, '\\', 'n')
		case '"':
			// %q 已经处理 quote 转义；这里不再二次处理
			out = append(out, '"')
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
