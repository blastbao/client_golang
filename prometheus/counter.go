// Copyright 2014 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prometheus

import (
	"errors"
	"math"
	"sync/atomic"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// Counter is a Metric that represents a single numerical value that only ever goes up.
//
// That implies that it cannot be used to count items whose number can also go down,
// e.g. the number of currently running goroutines.
// Those "counters" are represented by Gauges.
//
// A Counter is typically used to count requests served, tasks completed, errors occurred, etc.
//
// To create Counter instances, use NewCounter.

type Counter interface {
	Metric			// 实现 Metric 接口，则可以被 Collector 导出给 Registry
	Collector		// 实现 Collector 接口，则可以被 Registry 收集

	// Inc increments the counter by 1.
	//
	// Use Add to increment it by arbitrary non-negative values.
	Inc()

	// Add adds the given value to the counter.
	//
	// It panics if the value is < 0.
	Add(float64)
}

// ExemplarAdder is implemented by Counters that offer the option of adding a
// value to the Counter together with an exemplar. Its AddWithExemplar method
// works like the Add method of the Counter interface but also replaces the
// currently saved exemplar (if any) with a new one, created from the provided
// value, the current time as timestamp, and the provided labels. Empty Labels
// will lead to a valid (label-less) exemplar. But if Labels is nil, the current
// exemplar is left in place. AddWithExemplar panics if the value is < 0, if any
// of the provided labels are invalid, or if the provided labels contain more
// than 64 runes in total.
type ExemplarAdder interface {
	AddWithExemplar(value float64, exemplar Labels)
}

// CounterOpts is an alias for Opts. See there for doc comments.
type CounterOpts Opts


// NewCounter creates a new Counter based on the provided CounterOpts.
//
// The returned implementation also implements ExemplarAdder.
// It is safe to perform the corresponding type assertion.
//
// The returned implementation tracks the counter value in two separate variables,
// a float64 and a uint64.
//
// The latter is used to track calls of the Inc method and calls of the Add method
// with a value that can be represented as a uint64.
//
// This allows atomic increments of the counter with optimal performance.
// (It is common to have an Inc call in very hot execution paths.)
//
// Both internal tracking values are added up in the Write method.
//
// This has to be taken into account when it comes to precision and overflow behavior.
//
//
func NewCounter(opts CounterOpts) Counter {

	//
	desc := NewDesc(
		BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		nil,
		opts.ConstLabels,
	)

	//
	result := &counter{
		desc: desc,
		labelPairs: desc.constLabelPairs,
		now: time.Now,
	}

	// 这里的 init 调用看着比较诡异，因为 counter 本身实现了 Metric 接口，
	// 又通过匿名包含 selfCollector 实现 Collector 接口，这里 init 实际上就是
	// 使 result 的 Collector 接口函数 Describe()/Collect() 能够正确运行：
	//	（1）result.Describe() 返回 result.Desc()
	// 	（2）result.Collect()  返回 result 本身
	//
	// 所以，counter 能够被 registry 收集和导出。
	//
	result.init(result) // Init self-collection.

	return result
}

type counter struct {

	// valBits contains the bits of the represented float64 value,
	// while valInt stores values that are exact integers.
	//
	// Both have to go first in the struct to guarantee alignment for atomic operations.
	// http://golang.org/pkg/sync/atomic/#pkg-note-BUG
	valBits uint64
	valInt  uint64



	// 通过匿名包含 selfCollector 以实现 Collector 接口
	selfCollector


	desc *Desc

	labelPairs []*dto.LabelPair
	exemplar   atomic.Value 		// Containing nil or a *dto.Exemplar.

	now func() time.Time 			// To mock out time.Now() for testing.
}

func (c *counter) Desc() *Desc {
	return c.desc
}

func (c *counter) Add(v float64) {

	if v < 0 {
		panic(errors.New("counter cannot decrease in value"))
	}

	ival := uint64(v)
	if float64(ival) == v {
		atomic.AddUint64(&c.valInt, ival)
		return
	}

	for {
		oldBits := atomic.LoadUint64(&c.valBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + v)
		if atomic.CompareAndSwapUint64(&c.valBits, oldBits, newBits) {
			return
		}
	}
}

func (c *counter) AddWithExemplar(v float64, e Labels) {
	c.Add(v)
	c.updateExemplar(v, e)
}

func (c *counter) Inc() {
	atomic.AddUint64(&c.valInt, 1)
}



// out 为传出参数
func (c *counter) Write(out *dto.Metric) error {

	// 值
	fval := math.Float64frombits(atomic.LoadUint64(&c.valBits))
	ival := atomic.LoadUint64(&c.valInt)
	val := fval + float64(ival)

	// ?
	var exemplar *dto.Exemplar
	if e := c.exemplar.Load(); e != nil {
		exemplar = e.(*dto.Exemplar)
	}

	//
	return populateMetric(CounterValue, val, c.labelPairs, exemplar, out)
}

func (c *counter) updateExemplar(v float64, l Labels) {

	if l == nil {
		return
	}

	e, err := newExemplar(v, c.now(), l)
	if err != nil {
		panic(err)
	}

	c.exemplar.Store(e)
}



// CounterVec is a Collector that bundles a set of Counters that all share the same Desc,
// but have different values for their variable labels.
//
// CounterVec 是一个 Collector ，它维护着一组使用相同 Desc 的 Counter ，因此这些 Counter 具有相同
// 的 variableLabels、constLabels 标签，但是各个标签的取值则可能不同。
//
//
// This is used if you want to count the same thing partitioned by various dimensions
// (e.g. number of HTTP requests, partitioned by response code and method).
//
// 如果你想对同一个指标的不同维度进行计数，则可以使用此对象。
// 例如，对 HTTP 请求按 响应码 和 方法 进行分别统计。
//
// Create instances with NewCounterVec.
type CounterVec struct {
	*metricVec // metricVec 实现了 Collector 接口，所以 CounterVec 也实现了 Collector 接口，可以被 Registry 收集
}


// NewCounterVec creates a new CounterVec based on the provided CounterOpts and
// partitioned by the given label names.
func NewCounterVec(opts CounterOpts, labelNames []string) *CounterVec {

	// 构造 metric 描述信息
	desc := NewDesc(
		BuildFQName(opts.Namespace, opts.Subsystem, opts.Name), // 全限定名称
		opts.Help,												// 帮助信息
		labelNames,												// 变量标签
		opts.ConstLabels,										// 常量标签
	)

	newCounterMetric := func(lvs ...string) Metric {
		// 新建的 metric 必须和 desc 具有相同的标签数
		if len(lvs) != len(desc.variableLabels) {
			panic(makeInconsistentCardinalityError(desc.fqName, desc.variableLabels, lvs))
		}
		// 构造 counter 对象
		result := &counter{
			desc: desc,
			labelPairs: makeLabelPairs(desc, lvs),
			now: time.Now,
		}
		// ...
		result.init(result) // Init self-collection.
		// ...
		return result
	}

	return &CounterVec{
		metricVec: newMetricVec(desc, newCounterMetric),
	}
}


// GetMetricWithLabelValues returns the Counter for the given slice of label values
// (same order as the VariableLabels in Desc).
//
// GetMetricWithLabelValues 返回与给定的标签值（数组）对应的 Counter 计数器。
// （与 Desc 中的 VariableLabels 标签的顺序相同）
//
//
// If that combination of label values is accessed for the first time,
// a new Counter is created.
//
// 如果当前这组标签值是第一次被访问，将创建一个新 Counter 计数器。
//
//
// It is possible to call this method without using the returned Counter to only
// create the new Counter but leave it at its starting value 0.
//
// See also the SummaryVec example.
//
//
// Keeping the Counter for later use is possible (and should be considered if
// performance is critical), but keep in mind that Reset, DeleteLabelValues and
// Delete can be used to delete the Counter from the CounterVec.
//
// 可以保留 Counter 以便以后使用（如果性能至关重要，则需要考虑），但请记住，
// 可以使用 Reset，DeleteLabelValues 和 Delete从CounterVec 来从 CounterVec 中删除 Counter 。
//
//
// In that case, the Counter will still exist, but it will not be exported anymore,
// even if a Counter with the same label values is created later.
//
// 在这种情况下，即使以后创建具有相同标签值的 Counter ，该 Counter 仍将存在，但不再导出。
//
//
// An error is returned if the number of label values is not the same as the
// number of VariableLabels in Desc (minus any curried labels).
//
// 如果标签值的数量与 Desc 中的 VariableLabels 的数量（减去任何已固化的标签）不同，则返回错误。
//
//
// Note that for more than one label value, this method is prone to mistakes
// caused by an incorrect order of arguments.
//
// 请注意，对于多个标签值，此方法很容易因参数顺序错误而导致错误。
//
//
// Consider GetMetricWith(Labels) as an alternative to avoid that type of mistake.
// For higher label numbers, the latter has a much more readable (albeit more verbose) syntax,
// but it comes with a performance overhead (for creating and processing the Labels map).
//
// 考虑使用 GetMetricWith(Labels) 作为替代方案来避免这种类型的错误。
// 对于更多的标签，后者具有更易读（尽管更冗长）的语法，但是它会带来性能开销（用于创建和处理 Labels map）。
//
// See also the GaugeVec example.
func (v *CounterVec) GetMetricWithLabelValues(lvs ...string) (Counter, error) {  // lvs => label values

	//
	metric, err := v.metricVec.getMetricWithLabelValues(lvs...)
	if metric != nil {
		return metric.(Counter), err
	}


	return nil, err
}

// GetMetricWith returns the Counter for the given Labels map (the label names
// must match those of the VariableLabels in Desc).
// GetMetricWith 返回给定 Labels map 对应的的 Counter（标签名称必须与 Desc 中的 VariableLabels 相匹配）。
//
// If that label map is accessed for the first time, a new Counter is created.
// 如果是第一次访问该 label map ，则会创建一个新的计数器。
//
// Implications of creating a Counter without using it and keeping the Counter
// for later use are the same as for GetMetricWithLabelValues.
// 可以创建 Counter 但不使用，仅保留该 Counter 供以后使用。这个逻辑与 GetMetricWithLabelValues 相同。
//
// An error is returned if the number and names of the Labels are inconsistent
// with those of the VariableLabels in Desc (minus any curried labels).
// 如果标签的 ID 和 name 与 Desc 中的 VariableLabels（减去任何已固化的标签） 不一致，则会返回错误。
//
// This method is used for the same purpose as GetMetricWithLabelValues(...string).
// 此方法的用途与 GetMetricWithLabelValues(...string) 相同。
//
// See there for pros and cons of the two methods.
// 可以查看 GetMetricWithLabelValues 的注释对比这两种方法的优缺点。
//
func (v *CounterVec) GetMetricWith(labels Labels) (Counter, error) {

	// 从 metricVec.metricMap.metrics{hash->Metrics} 中取出同 labels 一致的 Metric
	metric, err := v.metricVec.getMetricWith(labels)
	if metric != nil {
		return metric.(Counter), err
	}

	return nil, err
}

// WithLabelValues works as GetMetricWithLabelValues,
// but panics where GetMetricWithLabelValues would have returned an error.
//
// Not returning an error allows shortcuts like
//    myVec.WithLabelValues("404", "GET").Add(42)
func (v *CounterVec) WithLabelValues(lvs ...string) Counter {
	c, err := v.GetMetricWithLabelValues(lvs...)
	if err != nil {
		panic(err)
	}
	return c
}

// With works as GetMetricWith, but panics where GetMetricWithLabels would have
// returned an error.
//
// Not returning an error allows shortcuts like
//    myVec.With(prometheus.Labels{"code": "404", "method": "GET"}).Add(42)
func (v *CounterVec) With(labels Labels) Counter {
	c, err := v.GetMetricWith(labels)
	if err != nil {
		panic(err)
	}
	return c
}



// CurryWith returns a vector curried with the provided labels, i.e.
// the returned vector has those labels pre-set for all labeled operations performed on it.
//
// The cardinality of the curried vector is reduced accordingly.
//
// The order of the remaining labels stays the same (just with the curried labels
// taken out of the sequence – which is relevant for the
// (GetMetric)WithLabelValues methods).
//
// It is possible to curry a curried vector, but only with labels not yet used for currying before.
//
// The metrics contained in the CounterVec are shared between the curried and uncurried vectors.
//
// They are just accessed differently. Curried and uncurried vectors behave identically in terms of collection.
//
// Only one must be registered with a given registry (usually the uncurried version).
//
// The Reset method deletes all metrics, even if called on a curried vector.
func (v *CounterVec) CurryWith(labels Labels) (*CounterVec, error) {
	vec, err := v.curryWith(labels)
	if vec != nil {
		return &CounterVec{vec}, err
	}
	return nil, err
}

// MustCurryWith works as CurryWith but panics where CurryWith would have returned an error.
func (v *CounterVec) MustCurryWith(labels Labels) *CounterVec {
	vec, err := v.CurryWith(labels)
	if err != nil {
		panic(err)
	}
	return vec
}

// CounterFunc is a Counter whose value is determined at collect time by calling a provided function.
//
// To create CounterFunc instances, use NewCounterFunc.
type CounterFunc interface {
	Metric
	Collector
}




// NewCounterFunc creates a new CounterFunc based on the provided CounterOpts.
//
// The value reported is determined by calling the given function from within the Write method.
//
// Take into account that metric collection may happen concurrently.
//
// If that results in concurrent calls to Write, like in the case where a CounterFunc is directly registered with Prometheus,
//
// the provided function must be concurrency-safe.
//
// The function should also honor the contract for a Counter (values only go up, not down), but compliance will not be checked.
func NewCounterFunc(opts CounterOpts, function func() float64) CounterFunc {

	return newValueFunc(
		NewDesc(
			BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
			opts.Help,
			nil,
			opts.ConstLabels,
		),
		CounterValue,
		function,
	)

}
