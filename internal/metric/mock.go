// Copyright 2019, OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metric

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/api/core"
	apimetric "go.opentelemetry.io/otel/api/metric"
)

type (
	Handle struct {
		Instrument *Instrument
		LabelSet   *LabelSet
	}

	Instrument struct {
		Name       string
		Kind       Kind
		NumberKind core.NumberKind
		Opts       apimetric.Options
	}

	LabelSet struct {
		TheMeter *Meter
		Labels   map[core.Key]core.Value
	}

	Batch struct {
		// Measurement needs to be aligned for 64-bit atomic operations.
		Measurements []Measurement
		Ctx          context.Context
		LabelSet     *LabelSet
	}

	MeterProvider struct {
		lock       sync.Mutex
		registered map[string]*Meter
	}

	Meter struct {
		MeasurementBatches []Batch
		// Observers contains also unregistered
		// observers. Check the Dead field of the Observer to
		// figure out its status.
		Observers []*Observer
	}

	Kind int8

	Measurement struct {
		// Number needs to be aligned for 64-bit atomic operations.
		Number     core.Number
		Instrument *Instrument
	}

	observerResult struct {
		instrument *Instrument
	}

	int64ObserverResult struct {
		result observerResult
	}

	float64ObserverResult struct {
		result observerResult
	}

	observerCallback func(observerResult)

	Observer struct {
		Instrument *Instrument
		Meter      *Meter
		Dead       bool
		callback   observerCallback
	}
)

var (
	_ apimetric.InstrumentImpl        = &Instrument{}
	_ apimetric.BoundInstrumentImpl   = &Handle{}
	_ apimetric.LabelSet              = &LabelSet{}
	_ apimetric.Meter                 = &Meter{}
	_ apimetric.Int64Observer         = &Observer{}
	_ apimetric.Float64Observer       = &Observer{}
	_ apimetric.Int64ObserverResult   = int64ObserverResult{}
	_ apimetric.Float64ObserverResult = float64ObserverResult{}
)

const (
	KindCounter Kind = iota
	KindGauge
	KindMeasure
	KindObserver
)

func (o *Observer) Unregister() {
	o.Dead = true
}

func (r int64ObserverResult) Observe(value int64, labels apimetric.LabelSet) {
	r.result.observe(core.NewInt64Number(value), labels)
}

func (r float64ObserverResult) Observe(value float64, labels apimetric.LabelSet) {
	r.result.observe(core.NewFloat64Number(value), labels)
}

func (r observerResult) observe(number core.Number, labels apimetric.LabelSet) {
	r.instrument.RecordOne(context.Background(), number, labels)
}

func (i *Instrument) Bind(labels apimetric.LabelSet) apimetric.BoundInstrumentImpl {
	if ld, ok := labels.(apimetric.LabelSetDelegate); ok {
		labels = ld.Delegate()
	}
	return &Handle{
		Instrument: i,
		LabelSet:   labels.(*LabelSet),
	}
}

func (i *Instrument) RecordOne(ctx context.Context, number core.Number, labels apimetric.LabelSet) {
	if ld, ok := labels.(apimetric.LabelSetDelegate); ok {
		labels = ld.Delegate()
	}
	doRecordBatch(ctx, labels.(*LabelSet), i, number)
}

func (h *Handle) RecordOne(ctx context.Context, number core.Number) {
	doRecordBatch(ctx, h.LabelSet, h.Instrument, number)
}

func (h *Handle) Unbind() {
}

func doRecordBatch(ctx context.Context, labelSet *LabelSet, instrument *Instrument, number core.Number) {
	labelSet.TheMeter.recordMockBatch(ctx, labelSet, Measurement{
		Instrument: instrument,
		Number:     number,
	})
}

func (s *LabelSet) Meter() apimetric.Meter {
	return s.TheMeter
}

func NewProvider() *MeterProvider {
	return &MeterProvider{
		registered: map[string]*Meter{},
	}
}

func (p *MeterProvider) Meter(name string) apimetric.Meter {
	p.lock.Lock()
	defer p.lock.Unlock()

	if lookup, ok := p.registered[name]; ok {
		return lookup
	}
	m := NewMeter()
	p.registered[name] = m
	return m
}

func NewMeter() *Meter {
	return &Meter{}
}

func (m *Meter) Labels(labels ...core.KeyValue) apimetric.LabelSet {
	ul := make(map[core.Key]core.Value)
	for _, kv := range labels {
		ul[kv.Key] = kv.Value
	}
	return &LabelSet{
		TheMeter: m,
		Labels:   ul,
	}
}

func (m *Meter) NewInt64Counter(name string, cos ...apimetric.CounterOptionApplier) apimetric.Int64Counter {
	instrument := m.newCounterInstrument(name, core.Int64NumberKind, cos...)
	return apimetric.WrapInt64CounterInstrument(instrument)
}

func (m *Meter) NewFloat64Counter(name string, cos ...apimetric.CounterOptionApplier) apimetric.Float64Counter {
	instrument := m.newCounterInstrument(name, core.Float64NumberKind, cos...)
	return apimetric.WrapFloat64CounterInstrument(instrument)
}

func (m *Meter) newCounterInstrument(name string, numberKind core.NumberKind, cos ...apimetric.CounterOptionApplier) *Instrument {
	opts := apimetric.Options{}
	apimetric.ApplyCounterOptions(&opts, cos...)
	return &Instrument{
		Name:       name,
		Kind:       KindCounter,
		NumberKind: numberKind,
		Opts:       opts,
	}
}

func (m *Meter) NewInt64Gauge(name string, gos ...apimetric.GaugeOptionApplier) apimetric.Int64Gauge {
	instrument := m.newGaugeInstrument(name, core.Int64NumberKind, gos...)
	return apimetric.WrapInt64GaugeInstrument(instrument)
}

func (m *Meter) NewFloat64Gauge(name string, gos ...apimetric.GaugeOptionApplier) apimetric.Float64Gauge {
	instrument := m.newGaugeInstrument(name, core.Float64NumberKind, gos...)
	return apimetric.WrapFloat64GaugeInstrument(instrument)
}

func (m *Meter) newGaugeInstrument(name string, numberKind core.NumberKind, gos ...apimetric.GaugeOptionApplier) *Instrument {
	opts := apimetric.Options{}
	apimetric.ApplyGaugeOptions(&opts, gos...)
	return &Instrument{
		Name:       name,
		Kind:       KindGauge,
		NumberKind: numberKind,
		Opts:       opts,
	}
}

func (m *Meter) NewInt64Measure(name string, mos ...apimetric.MeasureOptionApplier) apimetric.Int64Measure {
	instrument := m.newMeasureInstrument(name, core.Int64NumberKind, mos...)
	return apimetric.WrapInt64MeasureInstrument(instrument)
}

func (m *Meter) NewFloat64Measure(name string, mos ...apimetric.MeasureOptionApplier) apimetric.Float64Measure {
	instrument := m.newMeasureInstrument(name, core.Float64NumberKind, mos...)
	return apimetric.WrapFloat64MeasureInstrument(instrument)
}

func (m *Meter) newMeasureInstrument(name string, numberKind core.NumberKind, mos ...apimetric.MeasureOptionApplier) *Instrument {
	opts := apimetric.Options{}
	apimetric.ApplyMeasureOptions(&opts, mos...)
	return &Instrument{
		Name:       name,
		Kind:       KindMeasure,
		NumberKind: numberKind,
		Opts:       opts,
	}
}

func (m *Meter) RegisterInt64Observer(name string, callback apimetric.Int64ObserverCallback, oos ...apimetric.ObserverOptionApplier) apimetric.Int64Observer {
	wrappedCallback := wrapInt64ObserverCallback(callback)
	return m.newObserver(name, wrappedCallback, core.Int64NumberKind, oos...)
}

func wrapInt64ObserverCallback(callback apimetric.Int64ObserverCallback) observerCallback {
	if callback == nil {
		return func(result observerResult) {}
	}
	return func(result observerResult) {
		typeSafeResult := int64ObserverResult{
			result: result,
		}
		callback(typeSafeResult)
	}
}

func (m *Meter) RegisterFloat64Observer(name string, callback apimetric.Float64ObserverCallback, oos ...apimetric.ObserverOptionApplier) apimetric.Float64Observer {
	wrappedCallback := wrapFloat64ObserverCallback(callback)
	return m.newObserver(name, wrappedCallback, core.Float64NumberKind, oos...)
}

func wrapFloat64ObserverCallback(callback apimetric.Float64ObserverCallback) observerCallback {
	if callback == nil {
		return func(result observerResult) {}
	}
	return func(result observerResult) {
		typeSafeResult := float64ObserverResult{
			result: result,
		}
		callback(typeSafeResult)
	}
}

func (m *Meter) newObserver(name string, callback observerCallback, numberKind core.NumberKind, oos ...apimetric.ObserverOptionApplier) *Observer {
	opts := apimetric.Options{}
	apimetric.ApplyObserverOptions(&opts, oos...)
	obs := &Observer{
		Instrument: &Instrument{
			Name:       name,
			Kind:       KindObserver,
			NumberKind: numberKind,
			Opts:       opts,
		},
		Meter:    m,
		Dead:     false,
		callback: callback,
	}
	m.Observers = append(m.Observers, obs)
	return obs
}

func (m *Meter) RecordBatch(ctx context.Context, labels apimetric.LabelSet, measurements ...apimetric.Measurement) {
	ourLabelSet := labels.(*LabelSet)
	mm := make([]Measurement, len(measurements))
	for i := 0; i < len(measurements); i++ {
		m := measurements[i]
		mm[i] = Measurement{
			Instrument: m.InstrumentImpl().(*Instrument),
			Number:     m.Number(),
		}
	}
	m.recordMockBatch(ctx, ourLabelSet, mm...)
}

func (m *Meter) recordMockBatch(ctx context.Context, labelSet *LabelSet, measurements ...Measurement) {
	m.MeasurementBatches = append(m.MeasurementBatches, Batch{
		Ctx:          ctx,
		LabelSet:     labelSet,
		Measurements: measurements,
	})
}

func (m *Meter) RunObservers() {
	for _, observer := range m.Observers {
		if observer.Dead {
			continue
		}
		observer.callback(observerResult{
			instrument: observer.Instrument,
		})
	}
}
