# Local development

## Prerequisites
```bash
brew install kind kubectl clusterctl
kind create cluster --name capi-mgmt
kubectl config use-context kind-capi-mgmt

clusterctl init --bootstrap kubeadm --control-plane kubeadm

make install
make run
```

At that point:

 - clusterctl installs the core CAPI controllers into the Kind cluster
 - make install installs your StackitCluster / StackitMachine CRDs
 - make run starts your controller on the host, talking to the Kind management cluster

How to test it: create normal CAPI objects that reference your infra objects. The provider waits for owner Cluster / Machine objects, so applying only the raw sample infra CRs is not enough.

Minimal infra-cluster smoke test:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1alpha1
kind: StackitCluster
metadata:
  name: demo
  namespace: default
spec:
  projectID: <stackit-project-id>
  region: eu01
  # optional:
  # networkID: <existing-network-id>
---
apiVersion: cluster.x-k8s.io/v1beta2
kind: Cluster
metadata:
  name: demo
  namespace: default
spec:
  infrastructureRef:
    apiGroup: infrastructure.cluster.x-k8s.io
    kind: StackitCluster
    name: demo
```

Apply it with:

```bash
kubectl apply -f demo-cluster.yaml
kubectl get stackitclusters
kubectl describe stackitcluster demo
```

If networkID is omitted, the provider now creates a managed STACKIT network automatically.

If you want the controller to run inside Kind instead of on your host:

```bash
make docker-build IMG=cluster-api-provider-stackit:dev
kind load docker-image cluster-api-provider-stackit:dev --name capi-mgmt
make deploy IMG=cluster-api-provider-stackit:dev
kubectl -n cluster-api-provider-stackit-system logs deploy/cluster-api-provider-stackit-controller-manager -c manager -f
```

That works, but auth is harder in-cluster because the current deployment manifest does not yet wire STACKIT credentials into the manager pod. For development, make run is the better path.

Authentication: the controller calls iaas.NewAPIClient() from stackit-sdk-go, so auth is whatever the SDK finds by default. Priority is:

1. explicit config in code
2. environment variables
3. ~/.stackit/credentials.json

And auth method priority is:

1. Workload Identity Federation
2. Service account key flow
3. Service account token flow (deprecated)

For local development, the easiest secure option is usually service account key flow:

```bash
export STACKIT_SERVICE_ACCOUNT_KEY_PATH="$HOME/.stackit/sa-key.json"
make run
```

Or with a credentials file:

```json
{
  "STACKIT_SERVICE_ACCOUNT_KEY_PATH": "/Users/you/.stackit/sa-key.json"
}
```

saved under: ` ~/.stackit/credentials.json`

For federation:

```bash
export STACKIT_FEDERATED_TOKEN_FILE=/path/to/oidc-token
export STACKIT_SERVICE_ACCOUNT_EMAIL=my-sa@sa.stackit.cloud
make run
```

Important limitation: this repo is not yet packaged as a full clusterctl init --infrastructure stackit provider with templates, and it does not yet bring up a full workload cluster end-to-end by itself. Right now, the practical local workflow is: Kind management cluster + core CAPI controllers + your STACKIT infra controller, then exercise StackitCluster / StackitMachine reconciliation from there.
