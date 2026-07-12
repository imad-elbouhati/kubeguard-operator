# kubeguard-operator

KubeGuard is a Kubernetes operator that continuously enforces resource and
compliance guardrails on `Deployments` across a cluster, and automatically
corrects any drift it finds.

## Description

Manually keeping every `Deployment` in a cluster within policy — sane replica
counts, CPU/memory limits, required labels — doesn't scale once you have more
than a handful of teams pushing manifests. KubeGuard introduces a custom
`GuardPolicy` custom resource that lets you declare those guardrails once and
have them enforced continuously, cluster-wide.

The controller watches both `GuardPolicy` objects and `Deployments`. On every
reconcile it:

1. Resolves which namespaces a `GuardPolicy` applies to, using an optional
   `namespaceSelector` (no selector = all namespaces).
2. Lists every `Deployment` in those namespaces and compares it against the
   policy's `maxReplicas`, `maxCPU`, `maxMemory` and `enforceLabels` fields.
3. Patches any `Deployment` that violates the policy back into compliance
   (clamping replicas/limits, adding missing labels) and stamps it with
   `platform.demo/last-corrected` and `platform.demo/corrected-by` annotations.
4. Emits a Kubernetes `Event` on the `GuardPolicy` describing what was
   corrected, and records the violation in `status.violations`.
5. Updates a `Reconciled` status condition summarizing the outcome.
6. Requeues every 5 minutes for continuous enforcement, and also reacts
   immediately to `Deployment` create/update events.

### Example

```yaml
apiVersion: platform.demo/v1
kind: GuardPolicy
metadata:
  name: default-guardrails
spec:
  namespaceSelector:
    matchLabels:
      guardpolicy/enforce: "true"
  maxReplicas: 5
  maxCPU: "1"
  maxMemory: "1Gi"
  enforceLabels:
    team: "platform"
```

Applying this will cap any `Deployment` in a labeled namespace at 5 replicas,
1 CPU / 1Gi memory per container, and ensure `team: platform` is present as a
label — auto-correcting anything that drifts out of bounds.

Check `status.violations` on the `GuardPolicy` (or `kubectl get gp` — the
resource has the short name `gp`) to see what's been corrected and when.

## Getting Started

### Prerequisites

- go version v1.23.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/kubeguard-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/kubeguard-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
> privileges or be logged in as admin.

**Create instances of your solution** You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

> **NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/kubeguard-operator:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f' to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/imad-elbouhati/kubeguard-operator/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v1-alpha
```

2. See that a chart was generated under 'dist/chart', and users
can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing

Issues and pull requests are welcome. Run `make test` before submitting a PR,
and `make help` for a full list of available `make` targets.

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

```
http://www.apache.org/licenses/LICENSE-2.0
```

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
