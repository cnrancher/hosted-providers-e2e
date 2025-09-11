package helper

import (
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/rancher-sandbox/ele-testhelpers/tools"
	"github.com/rancher/hosted-providers-e2e/hosted/helpers"
	"github.com/rancher/shepherd/clients/rancher"
	management "github.com/rancher/shepherd/clients/rancher/generated/management/v3"
	"github.com/rancher/shepherd/extensions/clusters"
	"github.com/rancher/shepherd/extensions/clusters/ack"
	"github.com/rancher/shepherd/pkg/config"
	namegen "github.com/rancher/shepherd/pkg/namegenerator"
)

// CreateACKHostedCluster is a helper function that creates an ACK hosted cluster
func CreateACKHostedCluster(client *rancher.Client, displayName, cloudCredentialID, kubernetesVersion string, updateFunc func(clusterConfig *ack.ClusterConfig)) (*management.Cluster, error) {
	var ackClusterConfig ack.ClusterConfig
	config.LoadConfig(ack.ACKClusterConfigConfigurationFileKey, &ackClusterConfig)

	ackClusterConfig.Name = displayName
	ackClusterConfig.KubernetesVersion = kubernetesVersion

	if updateFunc != nil {
		updateFunc(&ackClusterConfig)
	}
	ginkgo.GinkgoLogr.Info(fmt.Sprintf("Creating ACK cluster version %v", kubernetesVersion))

	return ack.CreateACKHostedCluster(client, displayName, cloudCredentialID, ackClusterConfig, false, false, false, false, nil)

}

func ListACKAllVersions(client *rancher.Client) (allVersions []string, err error) {
	serverVersion, err := helpers.GetRancherServerVersion(client)
	if err != nil {
		return
	}

	allVersions = []string{"1.31.9-aliyun.1", "1.32.7-aliyun.1", "1.33.3-aliyun.1"}

	switch {
	case strings.Contains(serverVersion, "2.12"):
		allVersions = []string{"1.31.9-aliyun.1", "1.32.7-aliyun.1", "1.33.3-aliyun.1"}
	case strings.Contains(serverVersion, "2.11"):
		allVersions = []string{"1.31.9-aliyun.1", "1.32.7-aliyun.1"}
	case strings.Contains(serverVersion, "2.10"):
		allVersions = []string{"1.31.9-aliyun.1"}
	}

	// as a safety net, we ensure all the versions are UI supported
	return helpers.FilterUIUnsupportedVersions(allVersions, client), nil
}

// GetK8sVersion returns the k8s version to be used by the test;
// this value can either be a variant of envvar DOWNSTREAM_K8S_MINOR_VERSION or the highest available version
// or second-highest minor version in case of upgrade scenarios
func GetK8sVersion(client *rancher.Client, forUpgrade bool) (string, error) {
	if k8sVersion := helpers.DownstreamK8sMinorVersion; k8sVersion != "" {
		return k8sVersion, nil
	}
	allVariants, err := ListACKAllVersions(client)
	if err != nil {
		return "", err
	}

	return helpers.DefaultK8sVersion(allVariants, forUpgrade)
}

func DeleteACKHostCluster(cluster *management.Cluster, client *rancher.Client) error {
	return client.Management.Cluster.Delete(cluster)
}

// UpgradeClusterKubernetesVersion upgrades the k8s version to the value defined by upgradeToVersion.
// if checkClusterConfig is set to true, it will validate that the cluster control plane has been upgrade successfully
func UpgradeClusterKubernetesVersion(cluster *management.Cluster, upgradeToVersion string, client *rancher.Client, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	upgradedCluster.ACKConfig.KubernetesVersion = upgradeToVersion

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		Expect(cluster.ACKConfig.KubernetesVersion).To(Equal(upgradeToVersion))

		// Check if the desired config has been applied in Rancher
		// Check if ACKConfig has correct KubernetesVersion after upgrade
		Eventually(func() bool {
			ginkgo.GinkgoLogr.Info("Waiting for k8s upgrade to appear in ACKStatus.UpstreamSpec & ACKConfig ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			ginkgo.GinkgoLogr.Info(fmt.Sprintf("UpstreamSpec.Version: %v, ACKConfig.Version %v, upgradeToVersion %v",
				cluster.ACKStatus.UpstreamSpec.KubernetesVersion, cluster.ACKConfig.KubernetesVersion, upgradeToVersion))
			return cluster.ACKStatus.UpstreamSpec.KubernetesVersion == upgradeToVersion && cluster.ACKConfig.KubernetesVersion == upgradeToVersion
		}, tools.SetTimeout(30*time.Minute), 30*time.Second).Should(BeTrue())
		ginkgo.GinkgoLogr.Info("Done Waiting for k8s upgrade to appear in ACKStatus.UpstreamSpec & ACKConfig")
	}
	return cluster, nil
}

// ScaleNodeGroup modifies the number of initialNodeCount of all the nodegroups as defined by nodeCount
// if wait is set to true, it will wait until the cluster finishes updating;
// if checkClusterConfig is set to true, it will validate that nodepool has been scaled successfully
func ScaleNodeGroup(cluster *management.Cluster, client *rancher.Client, nodeCount int64, wait, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	configNodePools := upgradedCluster.ACKConfig.NodePoolList
	for i := range configNodePools {
		configNodePools[i].InstancesNum = nodeCount
	}

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		configNodePools = cluster.ACKConfig.NodePoolList
		for i := range configNodePools {
			Expect(configNodePools[i].InstancesNum).To(BeNumerically("==", nodeCount))
		}
	}

	if wait {
		err = clusters.WaitClusterToBeUpgraded(client, cluster.ID)
		Expect(err).To(BeNil())
	}

	if checkClusterConfig {
		// check that the desired config is applied on Rancher
		Eventually(func() bool {
			ginkgo.GinkgoLogr.Info("Waiting for the node count change to appear in ACKStatus.UpstreamSpec ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			upstreamNodeGroups := cluster.ACKStatus.UpstreamSpec.NodePoolList
			for i := range upstreamNodeGroups {
				if ng := upstreamNodeGroups[i]; ng.InstancesNum != nodeCount {
					return false
				}
			}
			return true
		}, tools.SetTimeout(15*time.Minute), 10*time.Second).Should(BeTrue())
		ginkgo.GinkgoLogr.Info("Done waiting for the node count change to appear in ACKStatus.UpstreamSpec")
	}

	return cluster, nil
}

// AddNodePool adds a nodepool to the list; it uses the nodepool template defined in CATTLE_TEST_CONFIG file
// if checkClusterConfig is set to true, it will validate that nodepool has been added successfully
func AddNodePool(cluster *management.Cluster, increaseBy int, client *rancher.Client, wait, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	currentNodeGroupNumber := len(cluster.ACKConfig.NodePoolList)

	var ackClusterConfig management.ACKClusterConfigSpec
	config.LoadConfig(ack.ACKClusterConfigConfigurationFileKey, &ackClusterConfig)
	nodePools := ackClusterConfig.NodePoolList
	ngTemplate := nodePools[0]

	updateNodeGroupsList := cluster.ACKConfig.NodePoolList
	for i := 1; i <= increaseBy; i++ {
		newNodeGroup := management.NodePoolInfo{
			Name:               namegen.AppendRandomString("ng"),
			InstanceTypes:      ngTemplate.InstanceTypes,
			InstancesNum:       ngTemplate.InstancesNum,
			KeyPair:            ngTemplate.KeyPair,
			Platform:           ngTemplate.Platform,
			SystemDiskCategory: ngTemplate.SystemDiskCategory,
			SystemDiskSize:     ngTemplate.SystemDiskSize,
			VSwitchIds:         ngTemplate.VSwitchIds,
			Runtime:            ngTemplate.Runtime,
			RuntimeVersion:     ngTemplate.RuntimeVersion,
		}
		updateNodeGroupsList = append(updateNodeGroupsList, newNodeGroup)
	}
	upgradedCluster.ACKConfig.NodePoolList = updateNodeGroupsList

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		Expect(len(cluster.ACKConfig.NodePoolList)).Should(BeNumerically("==", currentNodeGroupNumber+increaseBy))
		for i, ng := range cluster.ACKConfig.NodePoolList {
			Expect(ng.Name).To(Equal(updateNodeGroupsList[i].Name))
		}
	}

	if wait {
		err = clusters.WaitClusterToBeUpgraded(client, cluster.ID)
		Expect(err).To(BeNil())
	}

	if checkClusterConfig {
		// Check if the desired config has been applied in Rancher
		Eventually(func() int {
			ginkgo.GinkgoLogr.Info("Waiting for the total nodepool count to increase in ACKStatus.UpstreamSpec ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			return len(cluster.ACKStatus.UpstreamSpec.NodePoolList)
		}, tools.SetTimeout(15*time.Minute), 10*time.Second).Should(BeNumerically("==", currentNodeGroupNumber+increaseBy))
		ginkgo.GinkgoLogr.Info("Done waiting for the total nodepool count to increase in ACKStatus.UpstreamSpec")

		for i, ng := range cluster.ACKStatus.UpstreamSpec.NodePoolList {
			Expect(ng.Name).To(Equal(updateNodeGroupsList[i].Name))
		}
	}

	return cluster, nil
}

// DeleteNodePool deletes a nodepool from the list
// if checkClusterConfig is set to true, it will validate that nodepool has been deleted successfully
func DeleteNodePool(cluster *management.Cluster, client *rancher.Client, wait, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	currentNodeGroupNumber := len(cluster.ACKConfig.NodePoolList)
	configNodePools := cluster.ACKConfig.NodePoolList
	updateNodeGroupsList := configNodePools[:1]
	upgradedCluster.ACKConfig.NodePoolList = updateNodeGroupsList

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		Expect(len(cluster.ACKConfig.NodePoolList)).Should(BeNumerically("==", currentNodeGroupNumber-1))
		for i, ng := range cluster.ACKConfig.NodePoolList {
			Expect(ng.Name).To(Equal(updateNodeGroupsList[i].Name))
		}
	}
	if wait {
		err = clusters.WaitClusterToBeUpgraded(client, cluster.ID)
		Expect(err).To(BeNil())
	}
	if checkClusterConfig {
		// Check if the desired config has been applied in Rancher
		Eventually(func() int {
			ginkgo.GinkgoLogr.Info("Waiting for the total nodepool count to decrease in ACKStatus.UpstreamSpec ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			return len(cluster.ACKStatus.UpstreamSpec.NodePoolList)
		}, tools.SetTimeout(15*time.Minute), 10*time.Second).Should(BeNumerically("==", currentNodeGroupNumber-1))
		for i, ng := range cluster.ACKStatus.UpstreamSpec.NodePoolList {
			Expect(ng.Name).To(Equal(updateNodeGroupsList[i].Name))
		}
		ginkgo.GinkgoLogr.Info("Done waiting for the total nodepool count to decrease in ACKStatus.UpstreamSpec")
	}
	return cluster, nil
}
