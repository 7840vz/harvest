/*
 * Copyright NetApp Inc, 2021 All rights reserved
 */

package fcp

import (
	"github.com/netapp/harvest/v2/cmd/poller/plugin"
	"github.com/netapp/harvest/v2/pkg/errs"
	"github.com/netapp/harvest/v2/pkg/matrix"
	"math"
	"strconv"
	"strings"
)

type Fcp struct {
	*plugin.AbstractPlugin
}

func New(p *plugin.AbstractPlugin) plugin.Plugin {
	return &Fcp{AbstractPlugin: p}
}

func (f *Fcp) Run(dataMap map[string]*matrix.Matrix) ([]*matrix.Matrix, error) {

	var rx, tx, util, read, write *matrix.Metric
	var err error

	data := dataMap[f.Object]
	if read = data.GetMetric("read_data"); read == nil {
		// Check for 7 mode fcp counters, as they start with fcp_.
		if read = data.GetMetric("fcp_read_data"); read == nil {
			return nil, errs.New(errs.ErrNoMetric, "read_data")
		}
	}

	if write = data.GetMetric("write_data"); write == nil {
		// Check for 7 mode fcp counters, as they start with fcp_.
		if write = data.GetMetric("fcp_write_data"); write == nil {
			return nil, errs.New(errs.ErrNoMetric, "write_data")
		}
	}

	if rx = data.GetMetric("read_percent"); rx == nil {
		if rx, err = data.NewMetricFloat64("read_percent"); err == nil {
			rx.SetProperty("raw")
		} else {
			return nil, err
		}

	}
	if tx = data.GetMetric("write_percent"); tx == nil {
		if tx, err = data.NewMetricFloat64("write_percent"); err == nil {
			tx.SetProperty("raw")
		} else {
			return nil, err
		}
	}

	if util = data.GetMetric("util_percent"); util == nil {
		if util, err = data.NewMetricFloat64("util_percent"); err == nil {
			util.SetProperty("raw")
		} else {
			return nil, err
		}
	}

	for _, instance := range data.GetInstances() {

		instance.SetLabel("port", strings.TrimPrefix(instance.GetLabel("port"), "port."))

		var speed int
		var s string
		var err error

		if speed, err = strconv.Atoi(instance.GetLabel("speed")); err != nil {
			f.Logger.Debug().Msgf("skip, can't convert speed (%s) to numeric", s)
		}

		if speed != 0 {

			var rxBytes, txBytes, rxPercent, txPercent float64
			var rxOk, txOk bool

			if rxBytes, rxOk = write.GetValueFloat64(instance); rxOk {
				rxPercent = rxBytes / float64(speed)
				err := rx.SetValueFloat64(instance, rxPercent)
				if err != nil {
					f.Logger.Error().Stack().Err(err).Msg("error")
				}
			}

			if txBytes, txOk = read.GetValueFloat64(instance); txOk {
				txPercent = txBytes / float64(speed)
				err := tx.SetValueFloat64(instance, txPercent)
				if err != nil {
					f.Logger.Error().Stack().Err(err).Msg("error")
				}
			}

			if rxOk || txOk {
				err := util.SetValueFloat64(instance, math.Max(rxPercent, txPercent))
				if err != nil {
					f.Logger.Error().Stack().Err(err).Msg("error")
				}
			}
		}
	}
	return nil, nil
}
