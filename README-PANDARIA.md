# HOSTED PROVIDERS E2E

## 本地运行测试

通用环境变量：

```sh
export PROVIDER=eks                    # ack/aks/cce/eks/tke
export CATTLE_TEST_CONFIG=cattle-config-provisioning.yaml
export RANCHER_HOSTNAME="1.2.3.4.sslip.io"
export RANCHER_PASSWORD=""             # ADMIN Password
export DOWNSTREAM_CLUSTER_CLEANUP=true
```

**AWSCN EKS Provisioning Tests**

```sh
#!/bin/bash

cd $(dirname $0)

# 编辑 eksClusterConfig
cp cattle-config-provisioning.example.yaml cattle-config-provisioning.yaml

export PROVIDER=eks
export RANCHER_HOSTNAME=1.2.3.4.sslip.io
export RANCHER_PASSWORD=admin123
export CATTLE_TEST_CONFIG=cattle-config-provisioning.yaml
export DOWNSTREAM_CLUSTER_CLEANUP=true

export EKS_REGION=ap-south-1
export AWS_ACCESS_KEY_ID=<key-id>
export AWS_SECRET_ACCESS_KEY=<key>

# 运行 P0 Provisioning Test
ginkgo run -vv -r --timeout=3h --keep-going --randomize-all --randomize-suites  --nodes 1 \
    --focus "P0Provisioning" ./hosted/${PROVIDER}/p0/
```

**HuaweiCloud CCE Provisioning Tests**

```sh
#!/bin/bash

cd $(dirname $0)

# 编辑 cceClusterConfig
cp cattle-config-provisioning.example.yaml cattle-config-provisioning.yaml

export PROVIDER=cce
export RANCHER_HOSTNAME=1.2.3.4.sslip.io
export RANCHER_PASSWORD=admin123
export CATTLE_TEST_CONFIG=cattle-config-provisioning.yaml
export DOWNSTREAM_CLUSTER_CLEANUP=true

export HUAWEI_ACCESS_KEY=<huawei_access_key>
export HUAWEI_SECRET_KEY=<huawei_secret_key>
export HUAWEI_PROJECT_ID=<huawei_project_id>
export CCE_REGION=ap-southeast-1

# 运行 P0 Provisioning Test
ginkgo run -vv -r --timeout=3h --keep-going --randomize-all --randomize-suites  --nodes 1 \
    --focus "P0Provisioning" ./hosted/${PROVIDER}/p0/
```