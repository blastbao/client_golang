// Copyright 2018 The Prometheus Authors
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
	"sort"

	"github.com/golang/protobuf/proto"

	dto "github.com/prometheus/client_model/go"
)






// WrapRegistererWith returns a Registerer wrapping the provided Registerer.
// Collectors registered with the returned Registerer will be registered with
// the wrapped Registerer in a modified way.
//
// The modified Collector adds the provided Labels to all Metrics it collects (as ConstLabels).
// The Metrics collected by the unmodified Collector must not duplicate any of those labels.
//
// WrapRegistererWith provides a way to add fixed labels to a subset of Collectors.
// It should not be used to add fixed labels to all metrics exposed.
//
// Conflicts between Collectors registered through the original Registerer with Collectors
// registered through the wrapping Registerer will still be detected.
//
// Any AlreadyRegisteredError returned by the Register method of either Registerer
// will contain the ExistingCollector in the form it was provided to the respective registry.
//
// The Collector example demonstrates a use of WrapRegistererWith.



// WrapRegistererWith 返回 Reg 的封装 wrappedReg ，注册在 wrappedReg 的 collector 会被做些修改后，再注册在 Reg 中。
//
// 这些 collector 会将标签 labels 添加到它收集的所有指标中(作为 constlabel )。
//
// WrapRegistererWith 提供了一种向某些 collector 添加 constlabel 的方法。但是不应该作为向 所有 导出的指标添加标签的方法。
//
//

func WrapRegistererWith(labels Labels, reg Registerer) Registerer {
	return &wrappingRegisterer{
		wrappedRegisterer: reg,
		labels:            labels,
	}
}


// WrapRegistererWithPrefix returns a Registerer wrapping the provided Registerer.
// Collectors registered with the returned Registerer will be
// registered with the wrapped Registerer in a modified way.

// The modified Collector adds the provided prefix to the name of all Metrics it collects.
//
// WrapRegistererWithPrefix is useful to have one place to prefix all metrics of a sub-system.
// To make this work, register metrics of the sub-system with the wrapping Registerer returned by WrapRegistererWithPrefix.
//
// It is rarely useful to use the same prefix for all metrics exposed.
//
// In particular, do not prefix metric names that are standardized across applications,
// as that would break horizontal monitoring, for example the metrics provided by the Go collector
// (see NewGoCollector) and the process collector (see NewProcessCollector).
// (In fact, those metrics are already prefixed with “go_” or “process_”, respectively.)
//
// Conflicts between Collectors registered through the original Registerer with
// Collectors registered through the wrapping Registerer will still be detected.
// Any AlreadyRegisteredError returned by the Register method of
// either Registerer will contain the ExistingCollector in the form it was
// provided to the respective registry.
func WrapRegistererWithPrefix(prefix string, reg Registerer) Registerer {
	return &wrappingRegisterer{
		wrappedRegisterer: reg,
		prefix:            prefix,
	}
}

type wrappingRegisterer struct {
	wrappedRegisterer Registerer
	prefix            string
	labels            Labels
}

func (r *wrappingRegisterer) Register(c Collector) error {
	return r.wrappedRegisterer.Register(&wrappingCollector{
		wrappedCollector: c,
		prefix:           r.prefix,
		labels:           r.labels,
	})
}

func (r *wrappingRegisterer) MustRegister(cs ...Collector) {
	for _, c := range cs {
		if err := r.Register(c); err != nil {
			panic(err)
		}
	}
}

func (r *wrappingRegisterer) Unregister(c Collector) bool {
	return r.wrappedRegisterer.Unregister(&wrappingCollector{
		wrappedCollector: c,
		prefix:           r.prefix,
		labels:           r.labels,
	})
}



type wrappingCollector struct {
	wrappedCollector Collector
	prefix           string
	labels           Labels
}

func (c *wrappingCollector) Collect(ch chan<- Metric) {

	metricsCh := make(chan Metric)

	// 调用 c.wrappedCollector.Collect() 收集原始 metrics，传入到管道 metricsCh 中
	go func() {
		c.wrappedCollector.Collect(metricsCh)
		close(metricsCh)
	}()

	// 从 metricsCh 中读取原始 metrics ，然后构造 wrappingMetric 对象传入到管道 ch 中
	for metric := range metricsCh {
		ch <- &wrappingMetric{
			wrappedMetric: metric,		// 原始 metric
			prefix:        c.prefix,	// 附加前缀
			labels:        c.labels,	// 附加标签
		}

	}
}

func (c *wrappingCollector) Describe(ch chan<- *Desc) {

	descsCh := make(chan *Desc)

	// 调用 c.wrappedCollector.Describe() 收集原始 descs，传入到管道 descsCh 中
	go func() {
		c.wrappedCollector.Describe(descsCh)
		close(descsCh)
	}()

	// 从 descsCh 中读取原始 descs ，然后构造 wrapDesc 对象传入到管道 ch 中
	for desc := range descsCh {
		ch <- wrapDesc(desc, c.prefix, c.labels)
	}
}

// 递归解封装，直到取出最原始的 collector 后返回
func (c *wrappingCollector) unwrapRecursively() Collector {
	switch wc := c.wrappedCollector.(type) {
	case *wrappingCollector:
		return wc.unwrapRecursively()
	default:
		return wc
	}
}

type wrappingMetric struct {
	wrappedMetric Metric
	prefix        string
	labels        Labels
}

func (m *wrappingMetric) Desc() *Desc {
	// 构造一个新的 desc:
	return wrapDesc(m.wrappedMetric.Desc(), m.prefix, m.labels)
}

func (m *wrappingMetric) Write(out *dto.Metric) error {

	// convert from Metric to *dto.Metric
	if err := m.wrappedMetric.Write(out); err != nil {
		return err
	}

	// 如果没有附加 labels，就不做额外处理
	if len(m.labels) == 0 {
		// No wrapping labels.
		return nil
	}

	// 如果附加了 labels，就追加到 out.Label 中
	for ln, lv := range m.labels {
		out.Label = append(out.Label, &dto.LabelPair{
			Name:  proto.String(ln),
			Value: proto.String(lv),
		})
	}

	// 把 out.Label 排序一下
	sort.Sort(labelPairSorter(out.Label))
	return nil
}


// 在 desc 基础上构造一个新的 desc:
// 1. 把 labels 作为 constLabels
// 2. 把 prefix + desc.fqName 作为新 fqName
func wrapDesc(desc *Desc, prefix string, labels Labels) *Desc {

	constLabels := Labels{}

	// 1. 取出 desc 的常量标签，存入 constLabels{}
	for _, lp := range desc.constLabelPairs {
		constLabels[*lp.Name] = *lp.Value
	}

	// 2. 把 labels 添加到 constLabels{} 中
	for ln, lv := range labels {
		// 如果遇到重复的标签，就报错返回
		if _, alreadyUsed := constLabels[ln]; alreadyUsed {
			return &Desc{
				fqName:          desc.fqName,
				help:            desc.help,
				variableLabels:  desc.variableLabels,
				constLabelPairs: desc.constLabelPairs,
				err:             fmt.Errorf("attempted wrapping with already existing label name %q", ln),
			}
		}
		constLabels[ln] = lv
	}

	// NewDesc will do remaining validations.
	// 3. 基于 desc 构造新的 desc
	newDesc := NewDesc(prefix + desc.fqName, desc.help, desc.variableLabels, constLabels)


	// Propagate errors if there was any.
	// This will override any errer created by NewDesc above, i.e. earlier errors get precedence.
	//
	//
	if desc.err != nil {
		newDesc.err = desc.err
	}


	return newDesc
}
