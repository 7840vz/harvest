/*
 * Copyright NetApp Inc, 2022 All rights reserved
 */

package snapmirror

import (
	"github.com/netapp/harvest/v2/cmd/collectors"
	"github.com/netapp/harvest/v2/cmd/poller/plugin"
	"github.com/netapp/harvest/v2/cmd/tools/rest"
	"github.com/netapp/harvest/v2/pkg/conf"
	"github.com/netapp/harvest/v2/pkg/matrix"
	"github.com/netapp/harvest/v2/pkg/tree/node"
	"strings"
	"time"
)

const PluginInvocationRate = 10

type SnapMirror struct {
	*plugin.AbstractPlugin
	data           *matrix.Matrix
	client         *rest.Client
	currentVal     int
	svmPeerDataMap map[string]Peer // [peer SVM alias name] -> [peer detail] map
}

type Peer struct {
	svm     string
	cluster string
}

func New(p *plugin.AbstractPlugin) plugin.Plugin {
	return &SnapMirror{AbstractPlugin: p}
}

func (my *SnapMirror) Init() error {

	var err error

	if err = my.InitAbc(); err != nil {
		return err
	}

	timeout, _ := time.ParseDuration(rest.DefaultTimeout)
	if my.client, err = rest.New(conf.ZapiPoller(my.ParentParams), timeout, my.Auth); err != nil {
		my.Logger.Error().Stack().Err(err).Msg("connecting")
		return err
	}

	if err = my.client.Init(5); err != nil {
		return err
	}

	my.svmPeerDataMap = make(map[string]Peer)

	my.data = matrix.New(my.Parent+".SnapMirror", "snapmirror", "snapmirror")

	exportOptions := node.NewS("export_options")
	instanceLabels := exportOptions.NewChildS("instance_labels", "")
	instanceKeys := exportOptions.NewChildS("instance_keys", "")

	if exportOption := my.ParentParams.GetChildS("export_options"); exportOption != nil {
		if exportedLabels := exportOption.GetChildS("instance_labels"); exportedLabels != nil {
			for _, label := range exportedLabels.GetAllChildContentS() {
				instanceLabels.NewChildS("", label)
			}
		}
		if exportedKeys := exportOption.GetChildS("instance_keys"); exportedKeys != nil {
			for _, key := range exportedKeys.GetAllChildContentS() {
				instanceKeys.NewChildS("", key)
			}
		}
	}
	my.data.SetExportOptions(exportOptions)

	// Assigned the value to currentVal so that plugin would be invoked first time to populate cache.
	my.currentVal = PluginInvocationRate
	return nil
}

func (my *SnapMirror) Run(dataMap map[string]*matrix.Matrix) ([]*matrix.Matrix, error) {
	// Purge and reset data
	data := dataMap[my.Object]
	my.data.PurgeInstances()
	my.data.Reset()

	// Set all global labels from Rest.go if already not exist
	my.data.SetGlobalLabels(data.GetGlobalLabels())

	if my.currentVal >= PluginInvocationRate {
		my.currentVal = 0

		if cluster, ok := data.GetGlobalLabels().GetHas("cluster"); ok {
			if err := my.getSVMPeerData(cluster); err != nil {
				return nil, err
			}
			my.Logger.Debug().Msg("updated svm peer detail")
		}
	}

	// update volume instance labels
	my.updateSMLabels(data)
	my.currentVal++

	return []*matrix.Matrix{my.data}, nil
}

func (my *SnapMirror) getSVMPeerData(cluster string) error {
	// Clean svmPeerMap map
	my.svmPeerDataMap = make(map[string]Peer)
	fields := []string{"name", "peer.svm.name", "peer.cluster.name"}
	query := "api/svm/peers"
	href := rest.BuildHref("", strings.Join(fields, ","), []string{"peer.cluster.name=!" + cluster}, "", "", "", "", query)

	result, err := rest.Fetch(my.client, href)
	if err != nil {
		my.Logger.Error().Err(err).Str("href", href).Msg("Failed to fetch data")
		return err
	}

	if len(result) == 0 {
		my.Logger.Debug().Msg("No svm peer found")
		return nil
	}

	for _, peerData := range result {
		localSvmName := peerData.Get("name").String()
		actualSvmName := peerData.Get("peer.svm.name").String()
		peerClusterName := peerData.Get("peer.cluster.name").String()
		my.svmPeerDataMap[localSvmName] = Peer{svm: actualSvmName, cluster: peerClusterName}
	}
	return nil
}

func (my *SnapMirror) updateSMLabels(data *matrix.Matrix) {
	var keys []string
	cluster, _ := data.GetGlobalLabels().GetHas("cluster")

	for key, instance := range data.GetInstances() {
		if instance.GetLabel("group_type") == "consistencygroup" {
			keys = append(keys, key)
		}
		vserverName := instance.GetLabel("source_vserver")

		// Update source_vserver in snapmirror (In case of inter-cluster SM - vserver name may differ)
		if peerDetail, ok := my.svmPeerDataMap[vserverName]; ok {
			instance.SetLabel("source_vserver", peerDetail.svm)
			instance.SetLabel("source_cluster", peerDetail.cluster)
		}

		if sourceCluster := instance.GetLabel("source_cluster"); sourceCluster == "" {
			instance.SetLabel("source_cluster", cluster)
			instance.SetLabel("local", "true")
		}

		// update the protectedBy and protectionSourceType fields and derivedRelationshipType in snapmirror_labels
		collectors.UpdateProtectedFields(instance)
	}

	// handle CG relationships
	my.handleCGRelationships(data, keys)

}

func (my *SnapMirror) handleCGRelationships(data *matrix.Matrix, keys []string) {

	for _, key := range keys {
		cgInstance := data.GetInstance(key)
		cgItemMappings := cgInstance.GetLabel("cg_item_mappings")
		// cg_item_mappings would be array of cgMapping. Example: vols1:@vold1,vols2:@vold2
		cgMappingData := strings.Split(cgItemMappings, ",")
		for _, cgMapping := range cgMappingData {
			var (
				cgVolumeInstance *matrix.Instance
				err              error
			)
			// cgMapping would be {source_volume}:@{destination volume}. Example: vols1:@vold1
			if volumes := strings.Split(cgMapping, ":@"); len(volumes) == 2 {
				sourceVol := volumes[0]
				destinationVol := volumes[1]
				/*
				 * cgVolumeInstanceKey: cgInstance's relationshipId + sourceVol + destinationVol
				 * Example:
				 * cgInstance's relationshipId: 958805a8-302a-11ed-a6ad-005056a79f6e, sourceVol: vols1, destinationVol: vold1.
				 * cgVolumeInstanceKey would be 958805a8-302a-11ed-a6ad-005056a79f6evols1vold1.
				 */
				cgVolumeInstanceKey := key + sourceVol + destinationVol

				if cgVolumeInstance, err = my.data.NewInstance(cgVolumeInstanceKey); err != nil {
					my.Logger.Error().Err(err).Str("Instance key", cgVolumeInstanceKey).Msg("")
					continue
				}

				for k, v := range cgInstance.GetLabels().Map() {
					cgVolumeInstance.SetLabel(k, v)
				}
				cgVolumeInstance.SetLabel("relationship_id", cgVolumeInstanceKey)
				cgVolumeInstance.SetLabel("source_volume", sourceVol)
				cgVolumeInstance.SetLabel("destination_volume", destinationVol)
			}
		}
	}

}
