package client

import (
	"context"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (data *data) LifecycleHook(
	ctx context.Context,
	operationType string,
	cluster client.Object,
	obj client.Object,
) error {
	var invokablePlugin []pluginData
	for _, plugin := range data.plugins {
		if slices.Contains(plugin.capabilities, obj.GetObjectKind().GroupVersionKind().Kind) {
			invokablePlugin = append(invokablePlugin, plugin)
		}
	}

	for _, plugin := range invokablePlugin {
		plugin.operatorClient.LifecycleHook()
	}

	return nil
}
