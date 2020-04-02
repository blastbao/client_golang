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
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cespare/xxhash"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/common/expfmt"

	dto "github.com/prometheus/client_model/go"

	"github.com/blastbao/client_golang/prometheus/internal"
)

const (
	// Capacity for the channel to collect metrics and descriptors.
	capMetricChan = 1000
	capDescChan   = 10
)

// DefaultRegisterer and DefaultGatherer are the implementations of the
// Registerer and Gatherer interface a number of convenience functions in this
// package act on. Initially, both variables point to the same Registry, which
// has a process collector (currently on Linux only, see NewProcessCollector)
// and a Go collector (see NewGoCollector, in particular the note about
// stop-the-world implication with Go versions older than 1.9) already
// registered. This approach to keep default instances as global state mirrors
// the approach of other packages in the Go standard library. Note that there
// are caveats. Change the variables with caution and only if you understand the
// consequences. Users who want to avoid global state altogether should not use
// the convenience functions and act on custom instances instead.
var (
	defaultRegistry              = NewRegistry()
	DefaultRegisterer Registerer = defaultRegistry
	DefaultGatherer   Gatherer   = defaultRegistry
)

func init() {
	MustRegister(NewProcessCollector(ProcessCollectorOpts{}))
	MustRegister(NewGoCollector())
}

// NewRegistry creates a new vanilla Registry without any Collectors pre-registered.
func NewRegistry() *Registry {
	return &Registry{
		collectorsByID:  map[uint64]Collector{},
		descIDs:         map[uint64]struct{}{},
		dimHashesByName: map[string]uint64{},
	}
}

// NewPedanticRegistry returns a registry that checks during collection if each
// collected Metric is consistent with its reported Desc, and if the Desc has
// actually been registered with the registry. Unchecked Collectors (those whose
// Describe method does not yield any descriptors) are excluded from the check.
//
// Usually, a Registry will be happy as long as the union of all collected
// Metrics is consistent and valid even if some metrics are not consistent with
// their own Desc or a Desc provided by their registered Collector. Well-behaved
// Collectors and Metrics will only provide consistent Descs. This Registry is
// useful to test the implementation of Collectors and Metrics.
func NewPedanticRegistry() *Registry {
	r := NewRegistry()
	r.pedanticChecksEnabled = true
	return r
}

// Registerer is the interface for the part of a registry in charge of
// registering and unregistering. Users of custom registries should use
// Registerer as type for registration purposes (rather than the Registry type
// directly). In that way, they are free to use custom Registerer implementation
// (e.g. for testing purposes).
type Registerer interface {


	// Register registers a new Collector to be included in metrics collection.
	//
	//
	//
	// It returns an error if the descriptors provided by the Collector are invalid or if they
	// — in combination with descriptors of already registered Collectors
	// — do not fulfill the consistency and uniqueness criteria described in the documentation of metric.Desc.
	//
	// If the provided Collector is equal to a Collector already registered
	// (which includes the case of re-registering the same Collector),
	// the returned error is an instance of AlreadyRegisteredError,
	// which contains the previously registered Collector.
	//
	// A Collector whose Describe method does not yield any Desc is treated as unchecked.
	// Registration will always succeed.
	//
	// No check for re-registering (see previous paragraph) is performed.
	// Thus, the caller is responsible for not double-registering the same unchecked Collector,
	// and for providing a Collector that will not cause inconsistent metrics on collection.
	// (This would lead to scrape errors.)


	// MustRegister 是注册 collector 最通用的方式。如果需要捕获注册时产生的错误，可以使用Register 函数，该函数会返回 error 而非 panic 。




	// 如果注册的 collector 与已经注册的 metric 不兼容或不一致时就会返回错误。
	//
	// registry 用于使收集的 metric 与 prometheus 数据模型保持一致，不一致的错误会在注册时而非采集时检测到。
	// 前者会在系统的启动时检测到，而后者只会在采集时发生（可能不会在首次采集时发生），这也是为什么 collector 和
	// metric 必须向 Registry describe 它们的原因。


	// 以上提到的 registry 都被称为默认 registry ，可以在全局变量 DefaultRegisterer 中找到。
	// 使用 NewRegistry 可以创建 custom registry，或者可以自己实现 Registerer 或 Gatherer 接口。
	// custom registry 的 Register 和 Unregister 运作方式类似，默认 registry 则使用全局函数 Register 和 Unregister 。

	// custom registry 的使用方式还有很多：
	// 可以使用 NewPedanticRegistry 来注册特殊的属性；
	// 可以避免由 DefaultRegisterer 限制的全局状态属性；
	// 也可以同时使用多个 registry 来暴露不同的metrics。

	// DefaultRegisterer 注册了 Go runtime metrics（通过 NewGoCollecto r）和用于 process metrics 的 collector
	// （通过NewProcessCollector）。
	// 通过custom registry可以自己决定注册的collector。



	// 注册一个需要包含在指标集中的收集器。
	//
	// 如果收集器提供的描述符非法、或者不满足 metric.Desc 的一致性/唯一性需求，则返回错误
	//
	// 如果相同的收集器已经注册过，返回 AlreadyRegisteredError ，其中包含先前注册的收集器的实例
	//
	// 其 Describe 方法不产生任何 Desc 的收集器，视为 Unchecked ，对这种收集器的注册总是成功的
	// 重现注册它时也不会有检查。因此，调用者必须负责确保不会重复注册

	Register(Collector) error




	// MustRegister works like Register but registers any number of Collectors and
	// panics upon the first registration that causes an error.
	//
	// 注册多个收集器，并且在遇到第一个失败时就 Panic
	MustRegister(...Collector)







	// Unregister unregisters the Collector that equals the Collector passed
	// in as an argument.  (Two Collectors are considered equal if their
	// Describe method yields the same set of descriptors.) The function
	// returns whether a Collector was unregistered. Note that an unchecked
	// Collector cannot be unregistered (as its Describe method does not
	// yield any descriptor).
	//
	// Note that even after unregistering, it will not be possible to
	// register a new Collector that is inconsistent with the unregistered
	// Collector, e.g. a Collector collecting metrics with the same name but
	// a different help string. The rationale here is that the same registry
	// instance must only collect consistent metrics throughout its
	// lifetime.
	//
	// 反注册
	Unregister(Collector) bool
}

// Gatherer is the interface for the part of a registry in charge of gathering
// the collected metrics into a number of MetricFamilies. The Gatherer interface
// comes with the same general implication as described for the Registerer
// interface.
type Gatherer interface {
	// Gather calls the Collect method of the registered Collectors and then
	// gathers the collected metrics into a lexicographically sorted slice
	// of uniquely named MetricFamily protobufs. Gather ensures that the
	// returned slice is valid and self-consistent so that it can be used
	// for valid exposition. As an exception to the strict consistency
	// requirements described for metric.Desc, Gather will tolerate
	// different sets of label names for metrics of the same metric family.
	//
	// Even if an error occurs, Gather attempts to gather as many metrics as
	// possible. Hence, if a non-nil error is returned, the returned
	// MetricFamily slice could be nil (in case of a fatal error that
	// prevented any meaningful metric collection) or contain a number of
	// MetricFamily protobufs, some of which might be incomplete, and some
	// might be missing altogether. The returned error (which might be a
	// MultiError) explains the details. Note that this is mostly useful for
	// debugging purposes. If the gathered protobufs are to be used for
	// exposition in actual monitoring, it is almost always better to not
	// expose an incomplete result and instead disregard the returned
	// MetricFamily protobufs in case the returned error is non-nil.
	Gather() ([]*dto.MetricFamily, error)
}

// Register registers the provided Collector with the DefaultRegisterer.
//
// Register is a shortcut for DefaultRegisterer.Register(c). See there for more
// details.
func Register(c Collector) error {
	return DefaultRegisterer.Register(c)
}

// MustRegister registers the provided Collectors with the DefaultRegisterer and
// panics if any error occurs.
//
// MustRegister is a shortcut for DefaultRegisterer.MustRegister(cs...). See
// there for more details.
func MustRegister(cs ...Collector) {
	DefaultRegisterer.MustRegister(cs...)
}

// Unregister removes the registration of the provided Collector from the
// DefaultRegisterer.
//
// Unregister is a shortcut for DefaultRegisterer.Unregister(c). See there for
// more details.
func Unregister(c Collector) bool {
	return DefaultRegisterer.Unregister(c)
}

// GathererFunc turns a function into a Gatherer.
type GathererFunc func() ([]*dto.MetricFamily, error)

// Gather implements Gatherer.
func (gf GathererFunc) Gather() ([]*dto.MetricFamily, error) {
	return gf()
}

// AlreadyRegisteredError is returned by the Register method if the Collector to
// be registered has already been registered before, or a different Collector
// that collects the same metrics has been registered before. Registration fails
// in that case, but you can detect from the kind of error what has
// happened. The error contains fields for the existing Collector and the
// (rejected) new Collector that equals the existing one. This can be used to
// find out if an equal Collector has been registered before and switch over to
// using the old one, as demonstrated in the example.
type AlreadyRegisteredError struct {
	ExistingCollector, NewCollector Collector
}

func (err AlreadyRegisteredError) Error() string {
	return "duplicate metrics collector registration attempted"
}


// MultiError is a slice of errors implementing the error interface.
//
// It is used by a Gatherer to report multiple errors during MetricFamily gathering.
type MultiError []error

func (errs MultiError) Error() string {

	if len(errs) == 0 {
		return ""
	}

	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "%d error(s) occurred:", len(errs))
	for _, err := range errs {
		fmt.Fprintf(buf, "\n* %s", err)
	}

	return buf.String()
}

// Append appends the provided error if it is not nil.
func (errs *MultiError) Append(err error) {
	if err != nil {
		*errs = append(*errs, err)
	}
}

// MaybeUnwrap returns nil if len(errs) is 0. It returns the first and only
// contained error as error if len(errs is 1). In all other cases, it returns
// the MultiError directly. This is helpful for returning a MultiError in a way
// that only uses the MultiError if needed.
func (errs MultiError) MaybeUnwrap() error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return errs[0]
	default:
		return errs
	}
}




// Registry registers Prometheus collectors, collects their metrics, and gathers them into MetricFamilies for exposition.
//
// It implements both Registerer and Gatherer.
//
// The zero value is not usable.
//
// Create instances with NewRegistry or NewPedanticRegistry.
//
//

type Registry struct {
	mtx                   sync.RWMutex
	collectorsByID        map[uint64]Collector // ID is a hash of the descIDs.
	descIDs               map[uint64]struct{}
	dimHashesByName       map[string]uint64
	uncheckedCollectors   []Collector
	pedanticChecksEnabled bool
}

// Register implements Registerer.
func (r *Registry) Register(c Collector) error {

	var (

		// 获取 c 所收集的 metrics 的 descs
		descChan           = make(chan *Desc, capDescChan)

		// 保存 c 所收集、新增的 metrics 的 descIds，因为有些 desc 可能已被注册，而新的 descIds 在完成注册前会加入到 r.descIDs[] 中。
		newDescIDs         = map[uint64]struct{}{}

		// ？
		newDimHashesByName = map[string]uint64{}

		// 计算 c 的 ID，该 ID 是 c.Describe() 返回的所有 desc.id (经过去重) 的异或，被当作 hash 值使用。
		collectorID        uint64 // All desc IDs XOR'd together.

		// 如果从 c.Describe() 中读到已注册的 desc ，则设置此错误
		duplicateDescErr   error
	)

	go func() {
		c.Describe(descChan)
		close(descChan)
	}()


	r.mtx.Lock()
	defer func() {
		// Drain channel in case of premature return to not leak a goroutine.
		for range descChan {
		}
		r.mtx.Unlock()
	}()


	// Conduct various tests...
	//
	// 从 c.Describe() 中读取 c 收集的 metrics 的 desc
	for desc := range descChan {

		// Is the descriptor valid at all?
		//
		// 如果 desc 在 NewDesc() 创建时出错，会在此时发现该错误。
		if desc.err != nil {
			return fmt.Errorf("descriptor %s is invalid: %s", desc, desc.err)
		}

		// Is the descID unique?
		// (In other words: Is the fqName + constLabel combination unique?)
		//
		// 检查当前 desc.id 是否已经注册在 r.descIDs 中，若已注册，意味着其它 Collector 已经注册了相同的 desc，因此要设置错误信息。
		if _, exists := r.descIDs[desc.id]; exists {
			duplicateDescErr = fmt.Errorf("descriptor %s already exists with the same fully-qualified name and const label values", desc)
		}

		// If it is not a duplicate desc in this collector, XOR it to the collectorID.
		// (We allow duplicate descs within the same collector, but their existence must be a no-op.)
		//
		// 至此，意味着 desc 此前未被其它 Collector 注册，需要将 desc 注册到 r 中。
		//
		// 检查当前 desc.id 是否已经被添加到 newDescIDs，若已经注册，则意味着从 c.Describe() 中读到重复的 desc，忽略后面重复的。
		// 若尚未注册，则将 desc.id 添加到 newDescIDs 中，同时更新 collectorID 的值（异或）。
		if _, exists := newDescIDs[desc.id]; !exists {
			newDescIDs[desc.id] = struct{}{}
			collectorID ^= desc.id	// collectorID 是 c.Describe() 返回的所有 desc.id (经过去重) 的异或值。
		}


		// Are all the label names and the help string consistent with previous descriptors of the same name?
		//
		// First check existing descriptors...
		//
		//
		//
		//
		if dimHash, exists := r.dimHashesByName[desc.fqName]; exists {

			//
			if dimHash != desc.dimHash {
				return fmt.Errorf("a previously registered descriptor with the same fully-qualified name as %s has different label names or a different help string", desc)
			}

		} else {

			// ...then check the new descriptors already seen.
			if dimHash, exists := newDimHashesByName[desc.fqName]; exists {

				if dimHash != desc.dimHash {
					return fmt.Errorf("descriptors reported by collector have inconsistent label names or help strings for the same fully-qualified name, offender is %s", desc)
				}

			} else {
				newDimHashesByName[desc.fqName] = desc.dimHash
			}

		}
	}


	// A Collector yielding no Desc at all is considered unchecked.
	//
	// 如果 c.Describe() 没有任何 Desc 返回，则视 c 为 "unchecked"，把 c 添加到 r.uncheckedCollectors[] 中，直接返回。
	if len(newDescIDs) == 0 {
		r.uncheckedCollectors = append(r.uncheckedCollectors, c)
		return nil
	}


	// 如果 collectorID 已经注册到本 Registry 中，就报错返回。
	if existing, exists := r.collectorsByID[collectorID]; exists {
		switch e := existing.(type) {
		case *wrappingCollector:
			return AlreadyRegisteredError{
				ExistingCollector: e.unwrapRecursively(),
				NewCollector:      c,
			}
		default:
			return AlreadyRegisteredError{
				ExistingCollector: e,
				NewCollector:      c,
			}
		}
	}



	// If the collectorID is new, but at least one of the descs existed before, we are in trouble.
	//
	// 至此，collectorID 尚未注册到 r 中，检查 duplicateDescErr 是否被设置。
	// 若被设置，意味着从 c.Describe() 读取的 desc 中有的已经被其它 Collector 注册过了，
	// 这属于错误情况，因为 prom 要求注册到同一个 registry 中的 collector 收集的 metric desc 不能重复，所以报错返回。
	//
	// 注意，为什么不在前面发现错误的时候就返回？
	//
	if duplicateDescErr != nil {
		return duplicateDescErr
	}



	// Only after all tests have passed, actually register.
	//
	// 将 c 注册到 r.collectorsByID 中
	r.collectorsByID[collectorID] = c


	// 将新增的 desc.ids 注册到 r.descIDs 中
	for descID := range newDescIDs {
		r.descIDs[descID] = struct{}{}
	}

	//
	for name, dimHash := range newDimHashesByName {
		r.dimHashesByName[name] = dimHash
	}


	return nil
}



// Unregister implements Registerer.
func (r *Registry) Unregister(c Collector) bool {
	var (
		descChan    = make(chan *Desc, capDescChan)
		descIDs     = map[uint64]struct{}{}
		collectorID uint64 // All desc IDs XOR'd together.
	)

	go func() {
		c.Describe(descChan)
		close(descChan)
	}()

	// 从 c.Describe() 中读取 c 收集的 metrics 的 desc
	for desc := range descChan {
		// 计算 collectorID: 该值是 c.Describe() 返回的所有 desc.id (经过去重) 的异或。
		// 这里 descIDs{} 用于去重 & 后续删除。
		if _, exists := descIDs[desc.id]; !exists {
			collectorID ^= desc.id
			descIDs[desc.id] = struct{}{}
		}
	}

	// 检查 collectorID 是否已经注册到 r 中，若未注册，则无需删除，直接返回。
	r.mtx.RLock()
	if _, exists := r.collectorsByID[collectorID]; !exists {
		r.mtx.RUnlock()
		return false
	}
	r.mtx.RUnlock()

	r.mtx.Lock()
	defer r.mtx.Unlock()

	// 从 r.collectorsByID 中删除 collectorID
	delete(r.collectorsByID, collectorID)

	// 从 r.descIDs 中删除 descIDs
	for id := range descIDs {
		delete(r.descIDs, id)
	}

	// dimHashesByName is left untouched as those must be consistent throughout the lifetime of a program.
	return true
}

// MustRegister implements Registerer.
func (r *Registry) MustRegister(cs ...Collector) {
	for _, c := range cs {
		if err := r.Register(c); err != nil {
			panic(err)
		}
	}
}

// Gather implements Gatherer.
func (r *Registry) Gather() ([]*dto.MetricFamily, error) {


	var (
		// 用于汇总 checked collectors 收集到的所有指标
		checkedMetricChan   = make(chan Metric, capMetricChan)
		// 用于汇总 unchecked collectors 收集到的所有指标
		uncheckedMetricChan = make(chan Metric, capMetricChan)


		metricHashes        = map[uint64]struct{}{}
		wg                  sync.WaitGroup
		errs                MultiError          // The collected errors to return in the end.
		registeredDescIDs   map[uint64]struct{} // Only used for pedantic checks
	)

	r.mtx.RLock()


	// 可能存在多个 collectors，为提高指标收集效率，需要并发收集，这里 goroutineBudget 为最高并发度，
	// 其值为 collectors 的总数，也即最高并发下，每个 collector 一个收集协程。
	goroutineBudget 		:= len(r.collectorsByID) + len(r.uncheckedCollectors)


	// 所有被收集的 metrics 会被汇总到 metricFamiliesByName 中
	metricFamiliesByName 	:= make(map[string]*dto.MetricFamily, len(r.dimHashesByName))


	checkedCollectors 		:= make(chan Collector, len(r.collectorsByID))
	uncheckedCollectors 	:= make(chan Collector, len(r.uncheckedCollectors))
	for _, collector := range r.collectorsByID {
		checkedCollectors <- collector
	}
	for _, collector := range r.uncheckedCollectors {
		uncheckedCollectors <- collector
	}

	// In case pedantic checks are enabled, we have to copy the map before giving up the RLock.
	if r.pedanticChecksEnabled {
		registeredDescIDs = make(map[uint64]struct{}, len(r.descIDs))
		for id := range r.descIDs {
			registeredDescIDs[id] = struct{}{}
		}
	}

	r.mtx.RUnlock()

	// 一共有 goroutineBudget 个 collector 待收集，每收集完一个 collector 就 wg.Done() 一次
	wg.Add(goroutineBudget)


	// 这里 collectWorker 会从 checkedCollectors/uncheckedCollectors 中获取 collector ，并调用 collector.Collect() 执行指标收集工作。
	collectWorker := func() {

		// collector.Collect() 会把 collector 收集的指标传递到入参管道，并且在最后一个指标发送后 return 返回，该函数执行是同步、阻塞的。


		// 注意，这里 for loop 每次调用一个 collector 的 Collect() 函数执行同步、阻塞式的指标收集，是串行的执行。
		// 如果 collector 较多，或者某个 collector 的收集较慢，那么会影响整体的指标收集效率，因此需要启动多个 collectWorker 协程并行收集。

		for {
			select {
			case collector := <-checkedCollectors:
				collector.Collect(checkedMetricChan)
			case collector := <-uncheckedCollectors:
				collector.Collect(uncheckedMetricChan)
			default:
				// 如果 checkedCollectors 和 uncheckedCollectors 发生阻塞，意味着没有新的 collector 需要收集，直接 return 。
				return
			}
			// 每执行完一个 collector 的收集，就 wg.Done
			wg.Done()
		}
	}



	// Start the first worker now to make sure at least one is running.
	//
	// 启动第一个收集协程，确保至少有一个正在运行。
	go collectWorker()

	// 每启动一个收集协程，就 goroutineBudget--，当 goroutineBudget == 0 时不再启动新协程。
	goroutineBudget--

	// Close checkedMetricChan and uncheckedMetricChan once all collectors are collected.
	go func() {
		// 如果 wg.Wait() 返回，意味着所有的 collector 均收集完毕
		wg.Wait()
		// 注意，这两个管道是同时关闭的
		close(checkedMetricChan)
		close(uncheckedMetricChan)
	}()


	// Drain checkedMetricChan and uncheckedMetricChan in case of premature return.
	defer func() {
		if checkedMetricChan != nil {
			for range checkedMetricChan {
			}
		}
		if uncheckedMetricChan != nil {
			for range uncheckedMetricChan {
			}
		}
	}()

	// Copy the channel references so we can nil them out later to remove them from the select statements below.
	cmc := checkedMetricChan
	umc := uncheckedMetricChan

	// 调用 collector.Collect() 收集指标是同步、阻塞的，因此需要并发收集，但是汇总操作比较快，这里直接在 for loop 内处理。

	for {

		select {

		case metric, ok := <-cmc:

			// cmc is closed
			if !ok {
				cmc = nil
				break
			}

			// 把 metric 汇总到 metricFamiliesByName 中
			err := processMetric(
				metric,
				metricFamiliesByName,
				metricHashes,
				registeredDescIDs,
			)

			// 错误信息汇总
			errs.Append(err)

		case metric, ok := <-umc:

			// umc is closed
			if !ok {
				umc = nil
				break
			}

			// 把 metric 汇总到 metricFamiliesByName 中
			err := processMetric(
				metric,
				metricFamiliesByName,
				metricHashes,
				nil,
			)

			// 错误信息汇总
			errs.Append(err)

		default:


			// 运行到这，意味着 cmc 和 umc 发生阻塞，可能是 metrics 的生产速度不足，
			//
			//（1）如果此时 goroutineBudget > 0 且存在尚未被收集的 collector，则新增收集协程；
			//（2）如果此时已经启动了足够多的收集协程（每个 collector 一个协程），或者不存在未被收集的 collector，
			// 	  则启动新协程没有意义，只能同步阻塞的去等待 cmc、umc 中的指标数据，直到它们被关闭，结束收集。
			//
			//
			//


			// len(channel): 返回管道 channel 中未读数据的个数。
			if goroutineBudget <= 0 || len(checkedCollectors)+len(uncheckedCollectors) == 0 {


				// All collectors are already being worked on or we have already as many goroutines started as there are collectors.
				// Do the same as above, just without the default.

				//
				//
				//
				select {
				case metric, ok := <-cmc:

					if !ok {
						cmc = nil
						break
					}
					errs.Append(processMetric(
						metric, metricFamiliesByName,
						metricHashes,
						registeredDescIDs,
					))
				case metric, ok := <-umc:
					if !ok {
						umc = nil
						break
					}
					errs.Append(processMetric(
						metric, metricFamiliesByName,
						metricHashes,
						nil,
					))
				}
				break
			}

			// Start more workers.
			//
			// 新增收集协程，提高指标收集速率
			go collectWorker()

			// 每启动一个收集协程，就 goroutineBudget--，当 goroutineBudget == 0 时不再启动新协程。
			goroutineBudget--

			// 每启动一个新协程，可以通过 Gosched() 给新协程运行的机会。
			runtime.Gosched()

		}

		// Once both checkedMetricChan and uncheckdMetricChan are closed and drained,
		// the contraption above will nil out cmc and umc, and then we can leave the collect loop here.

		if cmc == nil && umc == nil {
			break
		}

	}

	// 把 metricFamiliesByName 排序后以数组形式返回
	return internal.NormalizeMetricFamilies(metricFamiliesByName), errs.MaybeUnwrap()
}







// WriteToTextfile calls Gather on the provided Gatherer, encodes the result in
// the Prometheus text format, and writes it to a temporary file.
// Upon success, the temporary file is renamed to the provided filename.
//
// 调用 g.Gather() 收集指标，将结果编码为 Prometheus 文本格式， 并写入临时文件。
// 一旦写入成功，临时文件将被重命名为指定文件名。
//
//
// This is intended for use with the textfile collector of the node exporter.
// Note that the node exporter expects the filename to be suffixed with ".prom".
//
//
func WriteToTextfile(filename string, g Gatherer) error {

	// 构造临时文件
	tmp, err := ioutil.TempFile(filepath.Dir(filename), filepath.Base(filename))
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())


	// 收集指标
	mfs, err := g.Gather()
	if err != nil {
		return err
	}

	// 以 Prometheus 文本格式写入临时文件
	for _, mf := range mfs {
		if _, err := expfmt.MetricFamilyToText(tmp, mf); err != nil {
			return err
		}
	}

	// 关闭临时文件
	if err := tmp.Close(); err != nil {
		return err
	}

	// 修改权限
	if err := os.Chmod(tmp.Name(), 0644); err != nil {
		return err
	}

	// 重命名
	return os.Rename(tmp.Name(), filename)
}





// processMetric is an internal helper method only used by the Gather method.
func processMetric(
	metric Metric,
	metricFamiliesByName map[string]*dto.MetricFamily,
	metricHashes map[uint64]struct{},
	registeredDescIDs map[uint64]struct{},
) error {

	//
	desc := metric.Desc()

	// Wrapped metrics collected by an unchecked Collector can have an invalid Desc.
	//
	//
	if desc.err != nil {
		return desc.err
	}


	// convert from Metric to *dto.Metric
	dtoMetric := &dto.Metric{}
	if err := metric.Write(dtoMetric); err != nil {
		return fmt.Errorf("error collecting metric %v: %s", desc, err)
	}

	//
	metricFamily, ok := metricFamiliesByName[desc.fqName]


	if ok { // Existing name.


		if metricFamily.GetHelp() != desc.help {
			return fmt.Errorf("collected metric %s %s has help %q but should have %q", desc.fqName, dtoMetric, desc.help, metricFamily.GetHelp())
		}

		// TODO(beorn7): Simplify switch once Desc has type.
		switch metricFamily.GetType() {
		case dto.MetricType_COUNTER:
			if dtoMetric.Counter == nil {
				return fmt.Errorf("collected metric %s %s should be a Counter", desc.fqName, dtoMetric, )
			}
		case dto.MetricType_GAUGE:
			if dtoMetric.Gauge == nil {
				return fmt.Errorf("collected metric %s %s should be a Gauge", desc.fqName, dtoMetric, )
			}
		case dto.MetricType_SUMMARY:
			if dtoMetric.Summary == nil {
				return fmt.Errorf("collected metric %s %s should be a Summary", desc.fqName, dtoMetric, )
			}
		case dto.MetricType_UNTYPED:
			if dtoMetric.Untyped == nil {
				return fmt.Errorf("collected metric %s %s should be Untyped", desc.fqName, dtoMetric, )
			}
		case dto.MetricType_HISTOGRAM:
			if dtoMetric.Histogram == nil {
				return fmt.Errorf("collected metric %s %s should be a Histogram", desc.fqName, dtoMetric, )
			}
		default:
			panic("encountered MetricFamily with invalid type")
		}



	} else { // New name.



		metricFamily = &dto.MetricFamily{}
		metricFamily.Name = proto.String(desc.fqName)
		metricFamily.Help = proto.String(desc.help)
		// TODO(beorn7): Simplify switch once Desc has type.
		switch {
		case dtoMetric.Gauge != nil:
			metricFamily.Type = dto.MetricType_GAUGE.Enum()
		case dtoMetric.Counter != nil:
			metricFamily.Type = dto.MetricType_COUNTER.Enum()
		case dtoMetric.Summary != nil:
			metricFamily.Type = dto.MetricType_SUMMARY.Enum()
		case dtoMetric.Untyped != nil:
			metricFamily.Type = dto.MetricType_UNTYPED.Enum()
		case dtoMetric.Histogram != nil:
			metricFamily.Type = dto.MetricType_HISTOGRAM.Enum()
		default:
			return fmt.Errorf("empty metric collected: %s", dtoMetric)
		}

		//
		if err := checkSuffixCollisions(metricFamily, metricFamiliesByName); err != nil {
			return err
		}

		metricFamiliesByName[desc.fqName] = metricFamily

	}

	//
	if err := checkMetricConsistency(metricFamily, dtoMetric, metricHashes); err != nil {
		return err
	}

	//
	if registeredDescIDs != nil {

		// Is the desc registered at all?
		if _, exist := registeredDescIDs[desc.id]; !exist {
			return fmt.Errorf("collected metric %s %s with unregistered descriptor %s", metricFamily.GetName(), dtoMetric, desc, )
		}

		if err := checkDescConsistency(metricFamily, dtoMetric, desc); err != nil {
			return err
		}

	}

	//
	metricFamily.Metric = append(metricFamily.Metric, dtoMetric)
	return nil
}

// Gatherers is a slice of Gatherer instances that implements the Gatherer
// interface itself. Its Gather method calls Gather on all Gatherers in the
// slice in order and returns the merged results. Errors returned from the
// Gather calls are all returned in a flattened MultiError. Duplicate and
// inconsistent Metrics are skipped (first occurrence in slice order wins) and
// reported in the returned error.
//
// Gatherers can be used to merge the Gather results from multiple Registries.
// It also provides a way to directly inject existing MetricFamily protobufs into
// the gathering by creating a custom Gatherer with a Gather method that simply
// returns the existing MetricFamily protobufs.
//
// Note that no registration is involved (in contrast to Collector registration),
// so obviously registration-time checks cannot happen. Any inconsistencies
// between the gathered MetricFamilies are reported as errors by the Gather method,
// and inconsistent Metrics are dropped. Invalid parts of the MetricFamilies
// (e.g. syntactically invalid metric or label names) will go undetected.
type Gatherers []Gatherer

// Gather implements Gatherer.
func (gs Gatherers) Gather() ([]*dto.MetricFamily, error) {

	var (
		// 用于存储来自多个 gatherer 的 []*metricFamily 的聚合后数据，[mfName]=> &dto.MetricFamily{ Metric: ... }
		metricFamiliesByName = map[string]*dto.MetricFamily{}
		metricHashes         = map[uint64]struct{}{}
		errs                 MultiError // The collected errors to return in the end.
	)

	for i, g := range gs {

		// 取出 g 的 metricFamilies
		mfs, err := g.Gather()
		if err != nil {
			if multiErr, ok := err.(MultiError); ok {
				for _, err := range multiErr {
					errs = append(errs, fmt.Errorf("[from Gatherer #%d] %s", i+1, err))
				}
			} else {
				errs = append(errs, fmt.Errorf("[from Gatherer #%d] %s", i+1, err))
			}
		}

		// 把 metricFamilies 中每个 metricFamily 都聚合存储到全局变量 metricFamiliesByName 上，
		// 注：PushGateway 上有相同的聚合逻辑。
		for _, mf := range mfs {

			existingMF, exists := metricFamiliesByName[mf.GetName()]
			if exists {
				if existingMF.GetHelp() != mf.GetHelp() {
					errs = append(errs, fmt.Errorf("gathered metric family %s has help %q but should have %q", mf.GetName(), mf.GetHelp(), existingMF.GetHelp()))
					continue
				}
				if existingMF.GetType() != mf.GetType() {
					errs = append(errs, fmt.Errorf("gathered metric family %s has type %s but should have %s", mf.GetName(), mf.GetType(), existingMF.GetType(), ))
					continue
				}
			} else {

				existingMF = &dto.MetricFamily{}
				existingMF.Name = mf.Name
				existingMF.Help = mf.Help
				existingMF.Type = mf.Type


				if err := checkSuffixCollisions(existingMF, metricFamiliesByName); err != nil {
					errs = append(errs, err)
					continue
				}

				metricFamiliesByName[mf.GetName()] = existingMF
			}

			// 聚合操作：将 mf.Metric 追加到 existingMF.Metric 中
			for _, m := range mf.Metric {

				//
				if err := checkMetricConsistency(existingMF, m, metricHashes); err != nil {
					errs = append(errs, err)
					continue
				}

				existingMF.Metric = append(existingMF.Metric, m)
			}
		}
	}

	// 将聚合结果按 mfName 排序后返回
	return internal.NormalizeMetricFamilies(metricFamiliesByName), errs.MaybeUnwrap()
}


// checkSuffixCollisions checks for collisions with the “magic” suffixes the
// Prometheus text format and the internal metric representation of the
// Prometheus server add while flattening Summaries and Histograms.
func checkSuffixCollisions(mf *dto.MetricFamily, mfs map[string]*dto.MetricFamily) error {


	var (
		newName              = mf.GetName()
		newType              = mf.GetType()
		newNameWithoutSuffix = ""
	)



	switch {
	case strings.HasSuffix(newName, "_count"):
		newNameWithoutSuffix = newName[:len(newName)-6]
	case strings.HasSuffix(newName, "_sum"):
		newNameWithoutSuffix = newName[:len(newName)-4]
	case strings.HasSuffix(newName, "_bucket"):
		newNameWithoutSuffix = newName[:len(newName)-7]
	}



	if newNameWithoutSuffix != "" {

		if existingMF, ok := mfs[newNameWithoutSuffix]; ok {
			switch existingMF.GetType() {
			case dto.MetricType_SUMMARY:
				if !strings.HasSuffix(newName, "_bucket") {
					return fmt.Errorf("collected metric named %q collides with previously collected summary named %q", newName, newNameWithoutSuffix)
				}
			case dto.MetricType_HISTOGRAM:
				return fmt.Errorf("collected metric named %q collides with previously collected histogram named %q", newName, newNameWithoutSuffix)
			}
		}

	}



	if newType == dto.MetricType_SUMMARY || newType == dto.MetricType_HISTOGRAM {
		if _, ok := mfs[newName+"_count"]; ok {
			return fmt.Errorf("collected histogram or summary named %q collides with previously collected metric named %q", newName, newName+"_count", )
		}
		if _, ok := mfs[newName+"_sum"]; ok {
			return fmt.Errorf("collected histogram or summary named %q collides with previously collected metric named %q", newName, newName+"_sum", )
		}
	}


	if newType == dto.MetricType_HISTOGRAM {
		if _, ok := mfs[newName+"_bucket"]; ok {
			return fmt.Errorf("collected histogram named %q collides with previously collected metric named %q", newName, newName+"_bucket", )
		}
	}

	return nil
}

// checkMetricConsistency checks if the provided Metric is consistent with the provided MetricFamily.
//
// It also hashes the Metric labels and the MetricFamily name.
//
// If the resulting hash is already in the provided metricHashes, an error is returned.
// If not, it is added to metricHashes.
//
//
func checkMetricConsistency(
	metricFamily *dto.MetricFamily,
	dtoMetric *dto.Metric,
	metricHashes map[uint64]struct{},
) error {


	name := metricFamily.GetName()

	// 检查 metric 类型是否一致

	// Metric type consistency with metric family.
	if  metricFamily.GetType() == dto.MetricType_GAUGE     && dtoMetric.Gauge 		== nil 		||
		metricFamily.GetType() == dto.MetricType_COUNTER   && dtoMetric.Counter 	== nil 		||
		metricFamily.GetType() == dto.MetricType_SUMMARY   && dtoMetric.Summary 	== nil 		||
		metricFamily.GetType() == dto.MetricType_HISTOGRAM && dtoMetric.Histogram 	== nil 		||
		metricFamily.GetType() == dto.MetricType_UNTYPED   && dtoMetric.Untyped 	== nil {

		return fmt.Errorf("collected metric %q { %s} is not a %s", name, dtoMetric, metricFamily.GetType(), )
	}

	// 检查 labels 是否合法
	previousLabelName := ""
	for _, labelPair := range dtoMetric.GetLabel() {

		labelName := labelPair.GetName()

		// 如果连续的两个 lable name 相同，意味着出现同名 label，报错
		if labelName == previousLabelName {
			return fmt.Errorf("collected metric %q { %s} has two or more labels with the same name: %s", name, dtoMetric, labelName, )
		}

		// 检查 label name 是否合法， 1. 合法字符串 2. 不能以 __ 开头
		if !checkLabelName(labelName) {
			return fmt.Errorf("collected metric %q { %s} has a label with an invalid name: %s", name, dtoMetric, labelName, )
		}

		// ...
		if dtoMetric.Summary != nil && labelName == quantileLabel {
			return fmt.Errorf("collected metric %q { %s} must not have an explicit %q label", name, dtoMetric, quantileLabel, )
		}

		// 检查 label value 是否是 utf8 编码
		if !utf8.ValidString(labelPair.GetValue()) {
			return fmt.Errorf("collected metric %q { %s} has a label named %q whose value is not utf8: %#v", name, dtoMetric, labelName, labelPair.GetValue())
		}

		previousLabelName = labelName
	}


	// 检查 labels 是否已排序，顺序会影响后续 hash 值计算

	// Make sure label pairs are sorted. We depend on it for the consistency check.
	if !sort.IsSorted(labelPairSorter(dtoMetric.Label)) {
		// We cannot sort dtoMetric.Label in place as it is immutable by contract.
		copiedLabels := make([]*dto.LabelPair, len(dtoMetric.Label))
		copy(copiedLabels, dtoMetric.Label)
		sort.Sort(labelPairSorter(copiedLabels))
		dtoMetric.Label = copiedLabels
	}

	// 计算 mfName + labels 对应的 hash 值

	// Is the metric unique (i.e. no other metric with the same name and the same labels)?
	h := xxhash.New()
	h.WriteString(name)
	h.Write(separatorByteSlice)
	for _, lp := range dtoMetric.Label {
		h.WriteString(lp.GetName())
		h.Write(separatorByteSlice)
		h.WriteString(lp.GetValue())
		h.Write(separatorByteSlice)
	}
	hSum := h.Sum64()


	// 如果 hash(mfName + labels) 已经存在，意味着当前 metric 已经收集，报错
	if _, exists := metricHashes[hSum]; exists {
		return fmt.Errorf("collected metric %q { %s} was collected before with the same name and label values", name, dtoMetric, )
	}

	// 保存
	metricHashes[hSum] = struct{}{}

	return nil
}

func checkDescConsistency(
	metricFamily *dto.MetricFamily,
	dtoMetric *dto.Metric,
	desc *Desc,
) error {


	// Desc help consistency with metric family help.
	if metricFamily.GetHelp() != desc.help {
		return fmt.Errorf("collected metric %s %s has help %q but should have %q", metricFamily.GetName(), dtoMetric, metricFamily.GetHelp(), desc.help, )
	}

	// Is the desc consistent with the content of the metric?
	lpsFromDesc := make([]*dto.LabelPair, len(desc.constLabelPairs), len(dtoMetric.Label))
	copy(lpsFromDesc, desc.constLabelPairs)
	for _, l := range desc.variableLabels {
		lpsFromDesc = append(lpsFromDesc, &dto.LabelPair{Name: proto.String(l)})
	}

	if len(lpsFromDesc) != len(dtoMetric.Label) {
		return fmt.Errorf("labels in collected metric %s %s are inconsistent with descriptor %s", metricFamily.GetName(), dtoMetric, desc, )
	}

	sort.Sort(labelPairSorter(lpsFromDesc))
	for i, lpFromDesc := range lpsFromDesc {
		lpFromMetric := dtoMetric.Label[i]
		if lpFromDesc.GetName() != lpFromMetric.GetName() ||
		  (lpFromDesc.Value != nil && lpFromDesc.GetValue() != lpFromMetric.GetValue()) {
			return fmt.Errorf("labels in collected metric %s %s are inconsistent with descriptor %s", metricFamily.GetName(), dtoMetric, desc, )
		}
	}
	return nil
}
