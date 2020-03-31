// Copyright 2016 The Prometheus Authors
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
	"fmt"
	"sort"
	"strings"

	"github.com/cespare/xxhash"
	"github.com/golang/protobuf/proto"
	"github.com/prometheus/common/model"

	dto "github.com/prometheus/client_model/go"
)







// Desc is the descriptor used by every Prometheus Metric.
//
// It is essentially(基本上) the immutable meta-data of a Metric.
//
// The normal Metric implementations included in this package manage their Desc under the hood.
//
// Users only have to deal with Desc if they use advanced features like the ExpvarCollector or
// custom Collectors and Metrics.
//
// [!]
// Descriptors registered with the same registry have to fulfill certain consistency and uniqueness
// criteria if they share the same fully-qualified name: They must have the same help string and the
// same label names (aka label dimensions) in each, constLabels and variableLabels, but they must
// differ in the values of the constLabels.
//
// [!]
// Descriptors that share the same fully-qualified names and the same label values of their constLabels are considered equal.
//
//
//
// Use NewDesc to create new Desc instances.
type Desc struct {


	// fqName has been built from Namespace, Subsystem, and Name.
	// 全限定名称，由命名空间-子系统-名称组成
	fqName string 	// fqName is the fully-qualified(完全限定的) name of the Metric


	// help provides some helpful information about this metric.
	// 指标的帮助信息
	help string


	// constLabelPairs contains precalculated DTO label pairs based on the constant labels.
	// 常量标签键值
	constLabelPairs []*dto.LabelPair


	// VariableLabels contains names of labels for which the metric maintains variable values.
	// 可变标签的名字
	variableLabels []string


	// id is a hash of the values of the ConstLabels and fqName.
	// This must be unique among all registered descriptors and can therefore be used as an identifier of the descriptor.
	//
	// 基于 ConstLabels 和 fqName 生成的哈希，所有注册的 Desc 都必须具有独特的 id 值，以作为 Desc 的唯一标识。
	id uint64

	// dimHash is a hash of the label names (preset and variable) and the Help string.
	// Each Desc with the same fqName must have the same dimHash.
	//
	// 维度哈希，所有 常量/可变 标签的哈希，所有具有相同 fqName 的 Desc 必须具有相同的 dimHash，这意味着每个 fqName 对应的标签集是固定的。
	dimHash uint64


	// err is an error that occurred during construction.
	// It is reported on registration time.
	//
	// 构造时出现的错误，注册时报告
	err error

}

// NewDesc allocates and initializes a new Desc. Errors are recorded in the Desc
// and will be reported on registration time. variableLabels and constLabels can
// be nil if no such labels should be set. fqName must not be empty.
//
// variableLabels only contain the label names. Their label values are variable
// and therefore not part of the Desc. (They are managed within the Metric.)
//
// For constLabels, the label values are constant. Therefore, they are fully
// specified in the Desc. See the Collector example for a usage pattern.
func NewDesc(fqName, help string, variableLabels []string, constLabels Labels) *Desc {


	d := &Desc{
		fqName:         fqName,
		help:           help,
		variableLabels: variableLabels,
	}

	if !model.IsValidMetricName(model.LabelValue(fqName)) {
		d.err = fmt.Errorf("%q is not a valid metric name", fqName)
		return d
	}

	// labelValues contains the label values of const labels
	// (in order of their sorted label names) plus the fqName (at position 0).
	labelValues := make([]string, 1, len(constLabels)+1)
	labelValues[0] = fqName

	labelNames := make([]string, 0, len(constLabels)+len(variableLabels))
	labelNameSet := map[string]struct{}{}

	// First add only the const label names and sort them...
	for labelName := range constLabels {

		if !checkLabelName(labelName) {
			d.err = fmt.Errorf("%q is not a valid label name for metric %q", labelName, fqName)
			return d
		}

		labelNames = append(labelNames, labelName)
		labelNameSet[labelName] = struct{}{}

	}


	sort.Strings(labelNames)


	// ... so that we can now add const label values in the order of their names.
	for _, labelName := range labelNames {
		labelValues = append(labelValues, constLabels[labelName])
	}


	// Validate the const label values. They can't have a wrong cardinality,
	// so use in len(labelValues) as expectedNumberOfValues.
	if err := validateLabelValues(labelValues, len(labelValues)); err != nil {
		// 如果在 NewDesc() 中构造 desc 出错，会在 Registry.Register() 时发现该错误。
		d.err = err
		return d
	}

	// Now add the variable label names, but prefix them with something that cannot be in a regular label name.
	//
	// That prevents matching the label dimension with a different mix between preset and variable labels.
	for _, labelName := range variableLabels {
		if !checkLabelName(labelName) {
			d.err = fmt.Errorf("%q is not a valid label name for metric %q", labelName, fqName)
			return d
		}
		labelNames = append(labelNames, "$"+labelName)
		labelNameSet[labelName] = struct{}{}
	}



	if len(labelNames) != len(labelNameSet) {
		d.err = errors.New("duplicate label names")
		return d
	}

	xxh := xxhash.New()
	for _, val := range labelValues {
		xxh.WriteString(val)
		xxh.Write(separatorByteSlice)
	}



	d.id = xxh.Sum64()

	// Sort labelNames so that order doesn't matter for the hash.
	sort.Strings(labelNames)


	// Now hash together (in this order) the help string and the sorted label names.
	xxh.Reset()
	xxh.WriteString(help)
	xxh.Write(separatorByteSlice)
	for _, labelName := range labelNames {
		xxh.WriteString(labelName)
		xxh.Write(separatorByteSlice)
	}
	d.dimHash = xxh.Sum64()

	d.constLabelPairs = make([]*dto.LabelPair, 0, len(constLabels))
	for n, v := range constLabels {
		d.constLabelPairs = append(d.constLabelPairs, &dto.LabelPair{
			Name:  proto.String(n),
			Value: proto.String(v),
		})
	}
	sort.Sort(labelPairSorter(d.constLabelPairs))
	return d


}

// NewInvalidDesc returns an invalid descriptor, i.e. a descriptor with the
// provided error set. If a collector returning such a descriptor is registered,
// registration will fail with the provided error. NewInvalidDesc can be used by
// a Collector to signal inability to describe itself.
func NewInvalidDesc(err error) *Desc {
	return &Desc{
		err: err,
	}
}

func (d *Desc) String() string {
	lpStrings := make([]string, 0, len(d.constLabelPairs))
	for _, lp := range d.constLabelPairs {
		lpStrings = append(
			lpStrings,
			fmt.Sprintf("%s=%q", lp.GetName(), lp.GetValue()),
		)
	}
	return fmt.Sprintf(
		"Desc{fqName: %q, help: %q, constLabels: {%s}, variableLabels: %v}",
		d.fqName,
		d.help,
		strings.Join(lpStrings, ","),
		d.variableLabels,
	)
}
