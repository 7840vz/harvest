package matrix

import (
	"fmt"
	"strconv"
	"goharvest2/share/errors"
	"goharvest2/share/util"
)

type MetricUint64 struct {
	*AbstractMetric
	values []uint64
}

// Storage resizing methods

func (m *MetricUint64) Reset(size int) {
	m.record = make([]bool, size)
	m.values = make([]uint64, size)
}

func (m *MetricUint64) Append() {
	m.record = append(m.record, false)
	m.values = append(m.values, 0)
}

// remove element at index, shift everything to left
func (m *MetricUint64) Remove(index int) {
	for i := index; i < len(m.values)-1; i++ {
		m.record[i] = m.record[i+1]
		m.values[i] = m.values[i+1]
	}
	m.record = m.record[:len(m.record)-1]
	m.values = m.values[:len(m.values)-1]
}


// Write methods 

func (m *MetricUint64) SetValueInt(i *Instance, v int) error {
	m.record[i.index] = true
	m.values[i.index] = uint64(v)
	return nil
}

func (m *MetricUint64) SetValueInt32(i *Instance, v int32) error {
	if v >= 0 {
		m.record[i.index] = true
		m.values[i.index] = uint64(v)
		return nil
	}
	return errors.New(OVERFLOW_ERROR, fmt.Sprintf("convert int32 (%d) to uint64", v))
}

func (m *MetricUint64) SetValueInt64(i *Instance, v int64) error {
	if v >= 0 {
		m.record[i.index] = true
		m.values[i.index] = uint64(v)
		return nil
	}
	return errors.New(OVERFLOW_ERROR, fmt.Sprintf("convert int64 (%d) to uint64", v))
}

func (m *MetricUint64) SetValueUint32(i *Instance, v uint32) error{
	m.record[i.index] = true
	m.values[i.index] = uint64(v)
	return nil
}

func (m *MetricUint64) SetValueUint64(i *Instance, v uint64) error {
	m.record[i.index] = true
	m.values[i.index] = v
	return nil
}

func (m *MetricUint64) SetValueFloat32(i *Instance, v float32) error {
	if v >= 0 {
		m.record[i.index] = true
		m.values[i.index] = uint64(v)
		return nil
	}
	return errors.New(OVERFLOW_ERROR, fmt.Sprintf("convert float32 (%f) to uint64", v))
}

func (m *MetricUint64) SetValueFloat64(i *Instance, v float64) error {
	if v >= 0 {
		m.record[i.index] = true
		m.values[i.index] = uint64(v)
		return nil
	}
	return errors.New(OVERFLOW_ERROR, fmt.Sprintf("convert float64 (%f) to uint64", v))
}

func (m *MetricUint64) SetValueString(i *Instance, v string) error {
	var x uint64
	var err error
	if x, err = strconv.ParseUint(v, 10, 32); err == nil {
		m.record[i.index] = true
		m.values[i.index] = x
		return nil
	}
	return err
}

func (m *MetricUint64) SetValueBytes(i *Instance, v []byte) error {
	return m.SetValueString(i, string(v))
}

// Read methods

func (m *MetricUint64) GetValueInt32(i *Instance) (int32, bool) {
	return int32(m.values[i.index]), m.record[i.index]
}

func (m *MetricUint64) GetValueInt64(i *Instance) (int64, bool) {
	return int64(m.values[i.index]), m.record[i.index]
}

func (m *MetricUint64) GetValueUint32(i *Instance) (uint32, bool) {
	return uint32(m.values[i.index]), m.record[i.index]
}

func (m *MetricUint64) GetValueUint64(i *Instance) (uint64, bool) {
	return m.values[i.index], m.record[i.index]
}

func (m *MetricUint64) GetValueFloat32(i *Instance) (float32, bool) {
	return float32(m.values[i.index]), m.record[i.index]
}

func (m *MetricUint64) GetValueFloat64(i *Instance) (float64, bool) {
	return float64(m.values[i.index]), m.record[i.index]
}

func (m *MetricUint64) GetValueString(i *Instance) (string, bool) {
	return strconv.FormatUint(m.values[i.index], 10), m.record[i.index]
}

func (m *MetricUint64) GetValueBytes(i *Instance) ([]byte, bool) {
	s, ok := m.GetValueString(i)
	return []byte(s), ok
}

func (m *MetricUint64) Print() {
	for i := range m.values {
		if m.record[i] {
			fmt.Printf("%s%v%s ", util.Green, m.values[i], util.End)
		} else {
			fmt.Printf("%s%v%s ", util.Red, m.values[i], util.End)
		}
	}
}