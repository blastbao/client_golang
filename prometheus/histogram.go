// Copyright 2015 The Prometheus Authors
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
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang/protobuf/proto"

	dto "github.com/prometheus/client_model/go"
)

// A Histogram counts individual observations from an event or sample stream in
// configurable buckets. Similar to a summary, it also provides a sum of
// observations and an observation count.
//
// On the Prometheus server, quantiles can be calculated from a Histogram using
// the histogram_quantile function in the query language.
//
// Note that Histograms, in contrast to Summaries, can be aggregated with the
// Prometheus query language (see the documentation for detailed
// procedures). However, Histograms require the user to pre-define suitable
// buckets, and they are in general less accurate. The Observe method of a
// Histogram has a very low performance overhead in comparison with the Observe
// method of a Summary.
//
// To create Histogram instances, use NewHistogram.
type Histogram interface {
	Metric
	Collector

	// Observe adds a single observation to the histogram.
	Observe(float64)
}


// bucketLabel is used for the label that defines the upper bound of a
// bucket of a histogram ("le" -> "less or equal").
const bucketLabel = "le"

// DefBuckets are the default Histogram buckets.
// The default buckets are tailored to broadly measure the response time (in seconds) of a network service.
// Most likely, however, you will be required to define buckets customized to your use case.
//
// 默认的 buckets 主要用于测量网络响应延迟（以秒为单位）的场景，对于其它场景，你应当自己定义 Buckets 。
//
var (
	DefBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	errBucketLabelNotAllowed = fmt.Errorf("%q is not allowed as label name in histograms", bucketLabel)
)



// LinearBuckets creates 'count' buckets, each 'width' wide, where the lowest bucket has an upper bound of 'start'.
//
// The final +Inf bucket is not counted and not included in the returned slice.
//
// The returned slice is meant to be used for the Buckets field of HistogramOpts.
//
// The function panics if 'count' is zero or negative.
func LinearBuckets(start, width float64, count int) []float64 {

	if count < 1 {
		panic("LinearBuckets needs a positive count")
	}

	// 创建 count 个 bucket：
	//   bucket[1] 		= start,
	//   bucket[2] 		= start + width,
	//   ...
	//   bucket[count] 	= start + (count-1) * width


	buckets := make([]float64, count)
	for i := range buckets {
		buckets[i] = start
		start += width
	}

	return buckets
}

// ExponentialBuckets creates 'count' buckets, where the lowest bucket has an
// upper bound of 'start' and each following bucket's upper bound is 'factor'
// times the previous bucket's upper bound.
//
// The final +Inf bucket is not counted and not included in the returned slice.
//
// The returned slice is meant to be used for the Buckets field of HistogramOpts.
//
// The function panics if 'count' is 0 or negative, if 'start' is 0 or negative,
// or if 'factor' is less than or equal 1.
func ExponentialBuckets(start, factor float64, count int) []float64 {

	if count < 1 {
		panic("ExponentialBuckets needs a positive count")
	}

	if start <= 0 {
		panic("ExponentialBuckets needs a positive start value")
	}

	if factor <= 1 {
		panic("ExponentialBuckets needs a factor greater than 1")
	}

	buckets := make([]float64, count)
	for i := range buckets {
		buckets[i] = start
		start *= factor
	}
	return buckets
}






// HistogramOpts bundles the options for creating a Histogram metric.
//
// It is mandatory to set Name to a non-empty string.
//
// All other fields are optional and can safely be left at their zero value,
// although it is strongly encouraged to set a Help string.
type HistogramOpts struct {

	// Namespace, Subsystem, and Name are components of the fully-qualified name of the Histogram
	// (created by joining these components with "_").
	//
	// Only Name is mandatory, the others merely help structuring the name.
	//
	// Note that the fully-qualified name of the Histogram must be a valid Prometheus metric name.
	Namespace string
	Subsystem string
	Name      string

	// Help provides information about this Histogram.
	//
	// Metrics with the same fully-qualified name must have the same Help string.
	Help string

	// ConstLabels are used to attach fixed labels to this metric.
	//
	// Metrics with the same fully-qualified name must have the same label names in their ConstLabels.
	//
	// ConstLabels are only used rarely.
	//
	// In particular, do not use them to attach the same labels to all your metrics.
	//
	// Those use cases are better covered by target labels set by the scraping Prometheus server,
	// or by one specific metric (e.g. a build_info or a machine_role metric).
	//
	// See also
	// https://prometheus.io/docs/instrumenting/writing_exporters/#target-labels-not-static-scraped-labels
	ConstLabels Labels



	// Buckets defines the buckets into which observations are counted.
	//
	// Each element in the slice is the upper inclusive bound of a bucket.
	//
	// The values must be sorted in strictly increasing order.
	//
	// There is no need to add a highest bucket with +Inf bound, it will be added implicitly.
	//
	// The default value is DefBuckets.

	Buckets []float64
}


// NewHistogram creates a new Histogram based on the provided HistogramOpts.
//
// It panics if the buckets in HistogramOpts are not in strictly increasing order.
//
// The returned implementation also implements ExemplarObserver.
//
// It is safe to perform the corresponding type assertion.
//
// Exemplars are tracked separately for each bucket.
func NewHistogram(opts HistogramOpts) Histogram {

	return newHistogram(
		NewDesc(
			BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
			opts.Help,
			nil,
			opts.ConstLabels,
		),
		opts,
	)
}

func newHistogram(desc *Desc, opts HistogramOpts, labelValues ...string) Histogram {


	// variable 标签数目检查
	if len(desc.variableLabels) != len(labelValues) {
		panic(makeInconsistentCardinalityError(desc.fqName, desc.variableLabels, labelValues))
	}

	// variable 标签中不可以包含 "le" 标签
	for _, n := range desc.variableLabels {
		if n == bucketLabel {
			panic(errBucketLabelNotAllowed)
		}
	}

	// const 标签中不可以包含 "le" 标签
	for _, lp := range desc.constLabelPairs {
		if lp.GetName() == bucketLabel {
			panic(errBucketLabelNotAllowed)
		}
	}

	// 如果指定了 buckets 就覆盖掉默认 buckets
	if len(opts.Buckets) == 0 {
		opts.Buckets = DefBuckets
	}

	h := &histogram{
		desc:        desc,
		upperBounds: opts.Buckets, //[!] 区间上边界
		labelPairs:  makeLabelPairs(desc, labelValues),
		counts:      [2]*histogramCounts{{}, {}},
		now:         time.Now,
	}

	// 边界合法性检查
	for i, upperBound := range h.upperBounds {
		if i < len(h.upperBounds)-1 {
			// 增序检查
			if upperBound >= h.upperBounds[i+1] {
				panic(fmt.Errorf("histogram buckets must be in increasing order: %f >= %f", upperBound, h.upperBounds[i+1]))
			}
		} else {
			// 不要显式的声明 "+Inf" 边界
			if math.IsInf(upperBound, +1) {
				// The +Inf bucket is implicit. Remove it here.
				h.upperBounds = h.upperBounds[:i]
			}
		}
	}



	// Finally we know the final length of h.upperBounds and can make buckets for both counts as well as exemplars:
	h.counts[0].buckets = make([]uint64, len(h.upperBounds))
	h.counts[1].buckets = make([]uint64, len(h.upperBounds))
	h.exemplars = make([]atomic.Value, len(h.upperBounds)+1)

	// Init self-collection.
	h.init(h)

	return h
}

type histogramCounts struct {

	// sumBits contains the bits of the float64 representing the sum of all observations.
	// sumBits and count have to go first in the struct to guarantee alignment for atomic operations.
	// http://golang.org/pkg/sync/atomic/#pkg-note-BUG


	// the sum of all observation,
	// this value is float type, but to use atomic operations on it, you have to save its bit pattern in a “uint64”
	sumBits uint64

	// the total number of observations
	count   uint64

	// the observed values
	buckets []uint64
}




// histogram 并不会保存数据采样点值，每个 bucket 只有个记录样本数的 counter（float64），
// 即 histogram 存储的是区间的样本数统计值，
// 因此客户端性能开销相比 Counter 和 Gauge 而言没有明显改变，适合高并发的数据收集。





type histogram struct {



	// countAndHotIdx enables lock-free writes with use of atomic updates.
	// The most significant bit is the hot index [0 or 1] of the count field below.
	// Observe calls update the hot one.
	//
	// All remaining bits count the number of Observe calls.
	//
	// Observe starts by incrementing this counter,
	// and finish by incrementing the count field in the respective histogramCounts,
	// as a marker for completion.
	//
	//
	// Calls of the Write method (which are non-mutating reads from the perspective of
	// he histogram) swap the hot–cold under the writeMtx lock.
	//
	//
	//
	// A cooldown is awaited (while locked) by comparing the number of
	// observations with the initiation count.
	// Once they match, then the last observation on the now cool one has completed.
	//
	// All cool fields must be merged into the new hot before releasing writeMtx.
	//
	// Fields with atomic access first! See alignment constraint:
	// http://golang.org/pkg/sync/atomic/#pkg-note-BUG


	// “hotIdx” is used to mark which of the two counts is the hot one.
	//
	// Since atomic operations only work on integers, use a uint32 as the smallest type that has atomic operations.
	//
	// If ‘hotIdx’ is even, counts zero is the hot counts.
	// If ‘hotIdx’ is odd , counts one  is the hot counts.
	//
	countAndHotIdx uint64

	selfCollector

	desc     *Desc
	writeMtx sync.Mutex // Only used in the Write method.


	// Two counts, one is "hot" for lock-free observations, the other is "cold" for writing out a dto.Metric.
	//
	// It has to be an array of pointers to guarantee 64bit alignment of the histogramCounts,
	//
	// see http://golang.org/pkg/sync/atomic/#pkg-note-BUG.
	//
	// The observations all work on the hot counts, and the cold counts are used in the read path.
	counts [2]*histogramCounts

	// 区间上边界
	upperBounds []float64

	//
	labelPairs  []*dto.LabelPair
	exemplars   []atomic.Value // One more than buckets (to include +Inf), each a *dto.Exemplar.

	now func() time.Time // To mock out time.Now() for testing.
}

func (h *histogram) Desc() *Desc {
	return h.desc
}

func (h *histogram) Observe(v float64) {
	h.observe(v, h.findBucket(v))
}

func (h *histogram) ObserveWithExemplar(v float64, e Labels) {
	i := h.findBucket(v)
	h.observe(v, i)
	h.updateExemplar(v, i, e)
}

func (h *histogram) Write(out *dto.Metric) error {



	//1.
	//2.
	//3.
	//4.
	//5.


	// For simplicity, we protect this whole method by a mutex.
	//
	// It is not in the hot path, i.e. Observe is called much more often than Write.
	//
	// The complication of making Write lock-free isn't worth it, if possible at all.
	h.writeMtx.Lock()
	defer h.writeMtx.Unlock()

	// Adding 1<<63 switches the hot index (from 0 to 1 or from 1 to 0) without touching the count bits.
	// See the struct comments for a full description of the algorithm.
	//
	// 给 h.countAndHotIdx 增加 1<<63 可以在不影响低 63 位的情况下，将最高位进行 0、1 切换（从 0 到 1 或从 1 到 0 ）。
	//
	// 这个最高位代表了 hotHistogramCounts 的索引下标，所以，这里相当于进行了冷热切换，
	// 使正在执行的 observe() 函数所持有的 hotHistogramCounts 变成了 coldHistogramCounts，
	//
	// 而这些切换前的 observe() 还在向 coldHistogramCounts 设置变量，需要等待它们都执行完毕后，
	// 才能从 coldHistogramCounts 读取出一致的数据。
	//
	// 如何知道这些 observe() 执行完毕了呢？
	//
	// 通过检查切换时刻的 realCount 和 coldHistogramCounts.count 是否相等来判断，如果二者相等，
	// 意味着这些 observe() 函数均已完成。
	n := atomic.AddUint64(&h.countAndHotIdx, 1<<63)


	// count is contained unchanged in the lower 63 bits.
	//
	// 取出低 63 位的值，记为 count，它代表在冷热切换前，observe() 被调用的次数 realCount
	count := n & ((1 << 63) - 1)


	// The most significant bit tells us which counts is hot.
	// The complement is thus the cold one.

	// n>>63: 取出 n 最高位（第 64 位）的值，0 或者 1，
	// (^n)>>63: (^n) 是 n 的按位取反，所以这里是取出 n 最高位的相反数， 1 或者 0

	hotCounts  := h.counts[  n >>63]
	coldCounts := h.counts[(^n)>>63]


	// Await cooldown.
	//
	// 等待冷热切换前调用的 observe() 函数均已执行完毕，当前 coldHistogramCounts 中数据是一致的
	for count != atomic.LoadUint64(&coldCounts.count) {
		runtime.Gosched() // Let observations get work done.
	}


	// 构造 Histogram 结构体，用于返回
	his := &dto.Histogram{
		// 桶
		Bucket:      make([]*dto.Bucket, len(h.upperBounds)),
		// 采样次数
		SampleCount: proto.Uint64(count),
		// 样本总数
		SampleSum:   proto.Float64(math.Float64frombits(atomic.LoadUint64(&coldCounts.sumBits))),
	}

	var cumCount uint64
	for i, upperBound := range h.upperBounds {

		// buckets[0] ...buckets[i] 的累计样本数
		cumCount += atomic.LoadUint64(&coldCounts.buckets[i])

		// 构造 Bucket 结构体
		his.Bucket[i] = &dto.Bucket{
			CumulativeCount: proto.Uint64(cumCount),
			UpperBound:      proto.Float64(upperBound),
		}

		// ...
		if e := h.exemplars[i].Load(); e != nil {
			his.Bucket[i].Exemplar = e.(*dto.Exemplar)
		}
	}



	// If there is an exemplar for the +Inf bucket, we have to add that bucket explicitly.
	if e := h.exemplars[len(h.upperBounds)].Load(); e != nil {
		b := &dto.Bucket{
			CumulativeCount: proto.Uint64(count),
			UpperBound:      proto.Float64(math.Inf(1)),
			Exemplar:        e.(*dto.Exemplar),
		}
		his.Bucket = append(his.Bucket, b)
	}

	// 构造返回值
	out.Histogram = his
	out.Label = h.labelPairs

	// Finally add all the cold counts to the new hot counts and reset the cold counts.


	// 冷热数据同步
	//
	// 冷热切换过程中，冷数据代表了当前一致的数据快照，热数据代表实时新增的数据，
	// 在完成冷热切换之后，需要把冷数据同步到热数据上，这样热数据就代表实时的数据。
	//
	//
	// 1. 同步 observe() 调用次数
	// 更新 hotCounts.count，因为它是从 0 开始计数的（增量），需要加上历史累计值
	atomic.AddUint64(&hotCounts.count, count)
	// 重置 coldCounts.count，这样当它变为 hotCounts 时，是从 0 开始计数的
	atomic.StoreUint64(&coldCounts.count, 0)


	// 2. 同步采样样本数
	for {
		oldBits := atomic.LoadUint64(&hotCounts.sumBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + his.GetSampleSum())
		// 更新 hotCounts.sumBits，因为它是从 0 开始计数的（增量），需要加上历史累计值
		if atomic.CompareAndSwapUint64(&hotCounts.sumBits, oldBits, newBits) {
			// 重置 coldCounts.sumBits，这样当它变为 hotCounts 时，是从 0 开始计数的
			atomic.StoreUint64(&coldCounts.sumBits, 0)
			break
		}
	}

	// 3. 同步 bucket
	// 遍历每个 bucket ，执行数据同步
	for i := range h.upperBounds {
		// 更新 hotCounts.buckets[i]，因为它是从 0 开始计数的，需要加上历史累计值
		atomic.AddUint64(&hotCounts.buckets[i], atomic.LoadUint64(&coldCounts.buckets[i]))
		// 重置 coldCounts.buckets[i]，这样当它变为 hotCounts 时，是从 0 开始计数的
		atomic.StoreUint64(&coldCounts.buckets[i], 0)
	}


	return nil
}






// findBucket returns the index of the bucket for the provided value, or len(h.upperBounds) for the +Inf bucket.
func (h *histogram) findBucket(v float64) int {

	// TODO(beorn7):
	//  For small numbers of buckets (<30), a linear search is slightly faster than the binary search.
	//  If we really care, we could switch from one search strategy to the other depending on the number
	//  of buckets.
	//
	// Microbenchmarks (BenchmarkHistogramNoLabels):
	// 11  buckets: 38.3 ns/op linear - binary 48.7 ns/op
	// 100 buckets: 78.1 ns/op linear - binary 54.9 ns/op
	// 300 buckets: 154  ns/op linear - binary 61.6 ns/op
	return sort.SearchFloat64s(h.upperBounds, v)
}


// observe is the implementation for Observe without the findBucket part.
func (h *histogram) observe(v float64, bucket int) {


	// 注意，h.countAndHotIdx 由两部分组成，高 1 位 hotIndex，低 63 位 realCount (调用 observe() 的实时总数)。
	//
	// 函数 observe() 中的 5 个步骤是非原子的，且它可能被高并发的调用，因此各个变量可能处于不一致状态。
	//
	// 值得注意的是，因为所涉及的都是加法操作，当并发调用均已结束时，各个变量肯定是一致的（因为不存在数值覆盖），
	//
	// 但是如何确定当前的数据是一致的呢？
	//
	// 如果不存在并发调用，在一次 observe() 调用中，第 1 步设置的 realCount 和第 5 步设置的 hc.count 数值应该是一致的，
	// 仔细想下，即便存在并发调用，如果有 realCount == hc.count，意味着当前各个变量都处于一致的状态，因为 2～3 步肯定都已经处理完。
	//
	// 但是，在高并发、高频率调用 observe() 的情况下，有没有可能始终无法满足 "realCount == hc.count" 情况，因为 realCount 是持续增加的，
	// 这样永远都等不到数据一致的时刻，所以需要有一个机制，来保证一定能读到数据一致时候的快照，即便这个快照可能落后于最新值。
	//
	//
	// 采用的方案是：
	//
	// 引入 hot/cold 两个数据副本，新调用的 observe() 函数写 hot 副本，当需要读取数据时，执行副本的冷热状态切换，
	// 切换前调用的 observe() 函数所写的副本变成 cold 状态，只需等待这些 observe() 函数退出后，从 cold 副本中读取
	// 的数据，就是一致的。因为 cold 副本不接受新的 observe() 函数的写入，所以一定会等到所有写者退出。
	//
	// 冷热切换后，新调用的 observe() 函数写新的 hot 副本，两个副本的数据是不一致的，所以执行冷热切换后，需要把 cold 副本的数据
	// 同步到 hot 副本上。
	//
	//
	//


	// observe 的执行步骤：
	//   1. 执行 h.countAndHotIdx + 1，这只会影响低 63 位的计数，这相当于执行 realCount + 1，也即更新 observe() 的被调用次数。
	//   2. 取出 hotHistogramCounts hc
	//   3. 执行 hc.buckets[bucket] + 1
	//   4. 执行 hc.sumBits + v
	//   5. 执行 hc.count + 1


	// We increment h.countAndHotIdx so that the counter in the lower 63 bits gets incremented.
	//
	// At the same time, we get the new value back, which we can use to find the currently-hot counts.


	// 1. 给 countAndHotIdx 加 1, 这只会影响低 63 位的计数，这个修改后的计数即为 cold count
	n := atomic.AddUint64(&h.countAndHotIdx, 1)



	// 注意，取出 n 之后，后面的操作都是基于这个 n 的，所以 observe 和 Write 的之间的同步，本质就是通过这第 1 步所实现的。




	// 2. n>>63 取出 n 的最高位（0 或者 1），它代表当前生效的 hot histogramCounts 对象下标，并取出它。
	hotHistogramCounts := h.counts[n>>63]

	// 3. 为 hotHistogramCounts.buckets[bucket] 增加计数
	if bucket < len(h.upperBounds) {
		atomic.AddUint64(&hotHistogramCounts.buckets[bucket], 1)
	}

	// 4. 对 hotHistogramCounts.sumBits 执行加 v 后再通过 CAS 赋值回去
	for {
		oldBits := atomic.LoadUint64(&hotHistogramCounts.sumBits)
		newBits := math.Float64bits(math.Float64frombits(oldBits) + v)
		if atomic.CompareAndSwapUint64(&hotHistogramCounts.sumBits, oldBits, newBits) {
			break
		}
	}

	// Increment count last as we take it as a signal that the observation is complete.
	//
	// 5. 为 hotHistogramCounts.count 增加计数
	atomic.AddUint64(&hotHistogramCounts.count, 1)
}

// updateExemplar replaces the exemplar for the provided bucket. With empty
// labels, it's a no-op. It panics if any of the labels is invalid.
func (h *histogram) updateExemplar(v float64, bucket int, l Labels) {
	if l == nil {
		return
	}
	e, err := newExemplar(v, h.now(), l)
	if err != nil {
		panic(err)
	}
	h.exemplars[bucket].Store(e)
}

// HistogramVec is a Collector that bundles a set of Histograms that all share the
// same Desc, but have different values for their variable labels. This is used
// if you want to count the same thing partitioned by various dimensions
// (e.g. HTTP request latencies, partitioned by status code and method). Create
// instances with NewHistogramVec.
type HistogramVec struct {
	*metricVec
}

// NewHistogramVec creates a new HistogramVec based on the provided HistogramOpts and
// partitioned by the given label names.
func NewHistogramVec(opts HistogramOpts, labelNames []string) *HistogramVec {
	desc := NewDesc(
		BuildFQName(opts.Namespace, opts.Subsystem, opts.Name),
		opts.Help,
		labelNames,
		opts.ConstLabels,
	)
	return &HistogramVec{
		metricVec: newMetricVec(desc, func(lvs ...string) Metric {
			return newHistogram(desc, opts, lvs...)
		}),
	}
}

// GetMetricWithLabelValues returns the Histogram for the given slice of label
// values (same order as the VariableLabels in Desc).
//
// If that combination of label values is accessed for the first time, a new Histogram is created.
//
//
//
// It is possible to call this method without using the returned Histogram to only
// create the new Histogram but leave it at its starting value, a Histogram without
// any observations.
//
// Keeping the Histogram for later use is possible (and should be considered if
// performance is critical), but keep in mind that Reset, DeleteLabelValues and
// Delete can be used to delete the Histogram from the HistogramVec. In that case, the
// Histogram will still exist, but it will not be exported anymore, even if a
// Histogram with the same label values is created later. See also the CounterVec
// example.
//
// An error is returned if the number of label values is not the same as the
// number of VariableLabels in Desc (minus any curried labels).
//
// Note that for more than one label value, this method is prone to mistakes
// caused by an incorrect order of arguments. Consider GetMetricWith(Labels) as
// an alternative to avoid that type of mistake. For higher label numbers, the
// latter has a much more readable (albeit more verbose) syntax, but it comes
// with a performance overhead (for creating and processing the Labels map).
// See also the GaugeVec example.
func (v *HistogramVec) GetMetricWithLabelValues(lvs ...string) (Observer, error) {
	metric, err := v.metricVec.getMetricWithLabelValues(lvs...)
	if metric != nil {
		return metric.(Observer), err
	}
	return nil, err
}

// GetMetricWith returns the Histogram for the given Labels map (the label names
// must match those of the VariableLabels in Desc).
//
// If that label map is accessed for the first time, a new Histogram is created.
// Implications of creating a Histogram without using it and keeping the Histogram
// for later use are the same as for GetMetricWithLabelValues.
//
// An error is returned if the number and names of the Labels are inconsistent
// with those of the VariableLabels in Desc (minus any curried labels).
//
// This method is used for the same purpose as GetMetricWithLabelValues(...string).
// See there for pros and cons of the two methods.
func (v *HistogramVec) GetMetricWith(labels Labels) (Observer, error) {
	metric, err := v.metricVec.getMetricWith(labels)
	if metric != nil {
		return metric.(Observer), err
	}
	return nil, err
}





// WithLabelValues works as GetMetricWithLabelValues, but panics where
// GetMetricWithLabelValues would have returned an error. Not returning an
// error allows shortcuts like
//     myVec.WithLabelValues("404", "GET").Observe(42.21)
func (v *HistogramVec) WithLabelValues(lvs ...string) Observer {
	h, err := v.GetMetricWithLabelValues(lvs...)
	if err != nil {
		panic(err)
	}
	return h
}



// With works as GetMetricWith but panics where GetMetricWithLabels would have
// returned an error. Not returning an error allows shortcuts like
//     myVec.With(prometheus.Labels{"code": "404", "method": "GET"}).Observe(42.21)
func (v *HistogramVec) With(labels Labels) Observer {
	h, err := v.GetMetricWith(labels)
	if err != nil {
		panic(err)
	}
	return h
}







// CurryWith returns a vector curried with the provided labels, i.e. the returned
// vector has those labels pre-set for all labeled operations performed on it.
// The cardinality of the curried vector is reduced accordingly.
//
//
// CurryWith 返回一个 vector ，该 vector 为对其执行的所有标记操作预先设置了这些标签。
//
// 相应地降低了curried向量的基数。
//
// The order of the remaining labels stays the same (just with the curried labels
// taken out of the sequence – which is relevant for the (GetMetric)WithLabelValues methods).
//
// It is possible to curry a curried vector, but only with labels not yet used for currying before.
//
// The metrics contained in the HistogramVec are shared between the curried and uncurried vectors.
//
// They are just accessed differently. Curried and uncurried
// vectors behave identically in terms of collection.
//
// Only one must be registered with a given registry (usually the uncurried version).
// The Reset method deletes all metrics, even if called on a curried vector.
//
//
//
//
func (v *HistogramVec) CurryWith(labels Labels) (ObserverVec, error) {
	vec, err := v.curryWith(labels)
	if vec != nil {
		return &HistogramVec{vec}, err
	}
	return nil, err
}

// MustCurryWith works as CurryWith but panics where CurryWith would have returned an error.
func (v *HistogramVec) MustCurryWith(labels Labels) ObserverVec {
	vec, err := v.CurryWith(labels)
	if err != nil {
		panic(err)
	}
	return vec
}




type constHistogram struct {
	desc       *Desc
	count      uint64
	sum        float64
	buckets    map[float64]uint64
	labelPairs []*dto.LabelPair
}

func (h *constHistogram) Desc() *Desc {
	return h.desc
}

func (h *constHistogram) Write(out *dto.Metric) error {
	his := &dto.Histogram{}
	buckets := make([]*dto.Bucket, 0, len(h.buckets))

	his.SampleCount = proto.Uint64(h.count)
	his.SampleSum = proto.Float64(h.sum)

	for upperBound, count := range h.buckets {
		buckets = append(buckets, &dto.Bucket{
			CumulativeCount: proto.Uint64(count),
			UpperBound:      proto.Float64(upperBound),
		})
	}

	if len(buckets) > 0 {
		sort.Sort(buckSort(buckets))
	}
	his.Bucket = buckets

	out.Histogram = his
	out.Label = h.labelPairs

	return nil
}

// NewConstHistogram returns a metric representing a Prometheus histogram with
// fixed values for the count, sum, and bucket counts.
//
// As those parameters cannot be changed, the returned value does not implement the Histogram
// interface (but only the Metric interface).
//
// Users of this package will not have much use for it in regular operations.
// However, when implementing custom Collectors, it is useful as a throw-away metric that
// is generated on the fly to send it to Prometheus in the Collect method.
//
// buckets is a map of upper bounds to cumulative counts, excluding the +Inf bucket.
//
// NewConstHistogram returns an error if the length of labelValues is not
// consistent with the variable labels in Desc or if Desc is invalid.
func NewConstHistogram(
	desc *Desc,
	count uint64,
	sum float64,
	buckets map[float64]uint64,
	labelValues ...string,
) (Metric, error) {
	if desc.err != nil {
		return nil, desc.err
	}
	if err := validateLabelValues(labelValues, len(desc.variableLabels)); err != nil {
		return nil, err
	}
	return &constHistogram{
		desc:       desc,
		count:      count,
		sum:        sum,
		buckets:    buckets,
		labelPairs: makeLabelPairs(desc, labelValues),
	}, nil
}

// MustNewConstHistogram is a version of NewConstHistogram that panics where
// NewConstMetric would have returned an error.
func MustNewConstHistogram(
	desc *Desc,
	count uint64,
	sum float64,
	buckets map[float64]uint64,
	labelValues ...string,
) Metric {
	m, err := NewConstHistogram(desc, count, sum, buckets, labelValues...)
	if err != nil {
		panic(err)
	}
	return m
}

type buckSort []*dto.Bucket

func (s buckSort) Len() int {
	return len(s)
}

func (s buckSort) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s buckSort) Less(i, j int) bool {
	return s[i].GetUpperBound() < s[j].GetUpperBound()
}
