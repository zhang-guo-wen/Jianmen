package store

import (
	"testing"

	"jianmen/internal/model"
)

func TestNormalizeContainerEndpointInputAllowsSSHWithoutAddress(t *testing.T) {
	input, err := normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH,
		HostID: "host-1", HostAccountID: "account-1",
	})
	if err != nil {
		t.Fatalf("normalize SSH endpoint: %v", err)
	}
	if input.Address != "" {
		t.Fatalf("SSH address = %q, want empty", input.Address)
	}
}

func TestNormalizeContainerEndpointInputRequiresSSHHostAndAccount(t *testing.T) {
	_, err := normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionSSH,
	})
	if err == nil {
		t.Fatal("SSH endpoint without host and account was accepted")
	}
	_, err = normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeContainerd, ConnectionMode: model.ContainerConnectionContainerd,
		HostID: "host-1",
	})
	if err == nil {
		t.Fatal("containerd endpoint without host account was accepted")
	}
}

func TestNormalizeContainerEndpointInputRequiresDockerAPIAddress(t *testing.T) {
	_, err := normalizeContainerEndpointInput(ContainerEndpointInput{
		Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI,
	})
	if err == nil {
		t.Fatal("Docker API endpoint without address was accepted")
	}
}
