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
	"github.com/rancher/shepherd/extensions/clusters/tke"
	"github.com/rancher/shepherd/pkg/config"
	namegen "github.com/rancher/shepherd/pkg/namegenerator"
)

// CreateTKEHostedCluster is a helper function that creates an TKE hosted cluster
func CreateTKEHostedCluster(client *rancher.Client, displayName, cloudCredentialID, kubernetesVersion string, id int64, updateFunc func(clusterConfig *tke.ClusterConfig)) (*management.Cluster, error) {
	var tkeClusterConfig tke.ClusterConfig
	config.LoadConfig(tke.TKEClusterConfigConfigurationFileKey, &tkeClusterConfig)

	tkeClusterConfig.ClusterBasicSettings.ClusterName = displayName
	tkeClusterConfig.ClusterBasicSettings.ClusterVersion = kubernetesVersion

	// Initialize ClusterCIDRSettings if nil
	if tkeClusterConfig.ClusterCIDRSettings == nil {
		tkeClusterConfig.ClusterCIDRSettings = &tke.ClusterCIDRSettings{}
	}
	tkeClusterConfig.ClusterCIDRSettings.ClusterCIDR = fmt.Sprintf("10.%v.0.0/16", id%255)

	if updateFunc != nil {
		updateFunc(&tkeClusterConfig)
	}
	ginkgo.GinkgoLogr.Info(fmt.Sprintf("Creating TKE cluster version %v ClusterCIDR %v", kubernetesVersion, tkeClusterConfig.ClusterCIDRSettings.ClusterCIDR))

	return tke.CreateTKEHostedCluster(client, displayName, cloudCredentialID, tkeClusterConfig, false, false, false, false, nil)
}

func ListTKEAllVersions(client *rancher.Client) (allVersions []string, err error) {
	serverVersion, err := helpers.GetRancherServerVersion(client)
	if err != nil {
		return
	}

	allVersions = []string{"1.32.2", "1.30.0", "1.28.3"}

	switch {
	case strings.Contains(serverVersion, "2.12"):
		allVersions = []string{"1.32.2", "1.30.0"}
	case strings.Contains(serverVersion, "2.11"):
		allVersions = []string{"1.32.2", "1.30.0"}
	case strings.Contains(serverVersion, "2.10"):
		allVersions = []string{"1.30.0", "1.28.3"}
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
	allVariants, err := ListTKEAllVersions(client)
	if err != nil {
		return "", err
	}

	return helpers.DefaultK8sVersion(allVariants, forUpgrade)
}

func DeleteTKEHostCluster(cluster *management.Cluster, client *rancher.Client) error {
	return client.Management.Cluster.Delete(cluster)
}

// UpgradeClusterKubernetesVersion upgrades the k8s version to the value defined by upgradeToVersion.
// if checkClusterConfig is set to true, it will validate that the cluster control plane has been upgrade successfully
func UpgradeClusterKubernetesVersion(cluster *management.Cluster, upgradeToVersion string, client *rancher.Client, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	upgradedCluster.TKEConfig.ClusterBasicSettings.ClusterVersion = upgradeToVersion

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		Expect(cluster.TKEConfig.ClusterBasicSettings.ClusterVersion).To(Equal(upgradeToVersion))

		// Check if the desired config has been applied in Rancher
		// Check if TKEConfig has correct KubernetesVersion after upgrade
		Eventually(func() bool {
			ginkgo.GinkgoLogr.Info("Waiting for k8s upgrade to appear in TKEStatus.UpstreamSpec & TKEConfig ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			ginkgo.GinkgoLogr.Info(fmt.Sprintf("UpstreamSpec.Version: %v, TKEConfig.Version %v, upgradeToVersion %v",
				cluster.TKEStatus.UpstreamSpec.ClusterBasicSettings.ClusterVersion, cluster.TKEConfig.ClusterBasicSettings.ClusterVersion, upgradeToVersion))
			return cluster.TKEStatus.UpstreamSpec.ClusterBasicSettings.ClusterVersion == upgradeToVersion && cluster.TKEConfig.ClusterBasicSettings.ClusterVersion == upgradeToVersion
		}, tools.SetTimeout(30*time.Minute), 30*time.Second).Should(BeTrue())
		ginkgo.GinkgoLogr.Info("Done Waiting for k8s upgrade to appear in TKEStatus.UpstreamSpec & TKEConfig")
	}
	return cluster, nil
}

// ScaleNodeGroup modifies the number of initialNodeCount of all the nodegroups as defined by nodeCount
// if wait is set to true, it will wait until the cluster finishes updating;
// if checkClusterConfig is set to true, it will validate that nodepool has been scaled successfully
func ScaleNodeGroup(cluster *management.Cluster, client *rancher.Client, nodeCount int64, wait, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	configNodePools := upgradedCluster.TKEConfig.NodePoolList
	for i := range configNodePools {
		configNodePools[i].AutoScalingGroupPara.DesiredCapacity = nodeCount
	}

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		configNodePools = cluster.TKEConfig.NodePoolList
		for i := range configNodePools {
			Expect(configNodePools[i].AutoScalingGroupPara.DesiredCapacity).To(BeNumerically("==", nodeCount))
		}
	}

	if wait {
		err = clusters.WaitClusterToBeUpgraded(client, cluster.ID)
		Expect(err).To(BeNil())
	}

	if checkClusterConfig {
		// check that the desired config is applied on Rancher
		Eventually(func() bool {
			ginkgo.GinkgoLogr.Info("Waiting for the node count change to appear in TKEStatus.UpstreamSpec ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			upstreamNodeGroups := cluster.TKEStatus.UpstreamSpec.NodePoolList
			for i := range upstreamNodeGroups {
				if ng := upstreamNodeGroups[i]; ng.AutoScalingGroupPara.DesiredCapacity != nodeCount {
					return false
				}
			}
			return true
		}, tools.SetTimeout(15*time.Minute), 10*time.Second).Should(BeTrue())
		ginkgo.GinkgoLogr.Info("Done waiting for the node count change to appear in TKEStatus.UpstreamSpec")
	}

	return cluster, nil
}

// AddNodePool adds a nodepool to the list; it uses the nodepool template defined in CATTLE_TEST_CONFIG file
// if checkClusterConfig is set to true, it will validate that nodepool has been added successfully
func AddNodePool(cluster *management.Cluster, increaseBy int, client *rancher.Client, wait, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	currentNodeGroupNumber := len(cluster.TKEConfig.NodePoolList)

	var tkeClusterConfig management.TKEClusterConfigSpec
	config.LoadConfig(tke.TKEClusterConfigConfigurationFileKey, &tkeClusterConfig)
	nodePools := tkeClusterConfig.NodePoolList
	ngTemplate := nodePools[0]

	updateNodeGroupsList := cluster.TKEConfig.NodePoolList
	for i := 1; i <= increaseBy; i++ {
		newNodeGroup := management.NodePoolDetail{
			ClusterID:  ngTemplate.ClusterID,
			NodePoolID: ngTemplate.NodePoolID,
			AutoScalingGroupPara: &management.AutoScalingGroupPara{
				AutoScalingGroupName: ngTemplate.AutoScalingGroupPara.AutoScalingGroupName,
				MaxSize:              ngTemplate.AutoScalingGroupPara.MaxSize,
				MinSize:              ngTemplate.AutoScalingGroupPara.MinSize,
				DesiredCapacity:      ngTemplate.AutoScalingGroupPara.DesiredCapacity,
				VpcID:                ngTemplate.AutoScalingGroupPara.VpcID,
				SubnetIDs:            ngTemplate.AutoScalingGroupPara.SubnetIDs,
			},

			LaunchConfigurePara: &management.LaunchConfigurePara{
				LaunchConfigurationName: ngTemplate.LaunchConfigurePara.LaunchConfigurationName,
				InstanceType:            ngTemplate.LaunchConfigurePara.InstanceType,
				SystemDisk:              ngTemplate.LaunchConfigurePara.SystemDisk,
				InternetChargeType:      ngTemplate.LaunchConfigurePara.InternetChargeType,
				InternetMaxBandwidthOut: ngTemplate.LaunchConfigurePara.InternetMaxBandwidthOut,
				PublicIpAssigned:        ngTemplate.LaunchConfigurePara.PublicIpAssigned,
				DataDisks:               ngTemplate.LaunchConfigurePara.DataDisks,
				KeyIDs:                  ngTemplate.LaunchConfigurePara.KeyIDs,
				SecurityGroupIDs:        ngTemplate.LaunchConfigurePara.SecurityGroupIDs,
				InstanceChargeType:      ngTemplate.LaunchConfigurePara.InstanceChargeType,
			},
			Name:               namegen.AppendRandomString("ng"),
			Labels:             ngTemplate.Labels,
			Taints:             ngTemplate.Taints,
			NodePoolOs:         ngTemplate.NodePoolOs,
			OsCustomizeType:    ngTemplate.OsCustomizeType,
			Tags:               ngTemplate.Tags,
			DeletionProtection: ngTemplate.DeletionProtection,
		}
		updateNodeGroupsList = append(updateNodeGroupsList, newNodeGroup)
	}
	upgradedCluster.TKEConfig.NodePoolList = updateNodeGroupsList

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		Expect(len(cluster.TKEConfig.NodePoolList)).Should(BeNumerically("==", currentNodeGroupNumber+increaseBy))
		for i, ng := range cluster.TKEConfig.NodePoolList {
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
			ginkgo.GinkgoLogr.Info("Waiting for the total nodepool count to increase in TKEStatus.UpstreamSpec ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			return len(cluster.TKEStatus.UpstreamSpec.NodePoolList)
		}, tools.SetTimeout(15*time.Minute), 10*time.Second).Should(BeNumerically("==", currentNodeGroupNumber+increaseBy))
		ginkgo.GinkgoLogr.Info("Done waiting for the total nodepool count to increase in TKEStatus.UpstreamSpec")

		for i, ng := range cluster.TKEStatus.UpstreamSpec.NodePoolList {
			Expect(ng.Name).To(Equal(updateNodeGroupsList[i].Name))
		}
	}

	return cluster, nil
}

// DeleteNodePool deletes a nodepool from the list
// if checkClusterConfig is set to true, it will validate that nodepool has been deleted successfully
func DeleteNodePool(cluster *management.Cluster, client *rancher.Client, wait, checkClusterConfig bool) (*management.Cluster, error) {
	upgradedCluster := cluster
	currentNodeGroupNumber := len(cluster.TKEConfig.NodePoolList)
	configNodePools := cluster.TKEConfig.NodePoolList
	updateNodeGroupsList := configNodePools[:1]
	upgradedCluster.TKEConfig.NodePoolList = updateNodeGroupsList

	cluster, err := client.Management.Cluster.Update(cluster, &upgradedCluster)
	Expect(err).To(BeNil())

	if checkClusterConfig {
		// Check if the desired config is set correctly
		Expect(len(cluster.TKEConfig.NodePoolList)).Should(BeNumerically("==", currentNodeGroupNumber-1))
		for i, ng := range cluster.TKEConfig.NodePoolList {
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
			ginkgo.GinkgoLogr.Info("Waiting for the total nodepool count to decrease in TKEStatus.UpstreamSpec ...")
			cluster, err = client.Management.Cluster.ByID(cluster.ID)
			Expect(err).To(BeNil())
			return len(cluster.TKEStatus.UpstreamSpec.NodePoolList)
		}, tools.SetTimeout(15*time.Minute), 10*time.Second).Should(BeNumerically("==", currentNodeGroupNumber-1))
		for i, ng := range cluster.TKEStatus.UpstreamSpec.NodePoolList {
			Expect(ng.Name).To(Equal(updateNodeGroupsList[i].Name))
		}
		ginkgo.GinkgoLogr.Info("Done waiting for the total nodepool count to decrease in TKEStatus.UpstreamSpec")
	}
	return cluster, nil
}
