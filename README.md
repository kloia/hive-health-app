# hive-health-app

## Deploy with Helm

The chart is in `helm/hive`. Each environment has its own values file in
`helm/envs/<env>/values.yaml` (`dev`, `prod`); add a folder to add an environment.
The target cluster and namespace are chosen at deploy time.

Database settings (host, port, user, name) are in the values files. The
password always comes from a Kubernetes Secret (default: `hive-db`, key
`DB_PASSWORD`); see `helm/hive/values.yaml` for both options.

```bash
# 1. Once per namespace: the database password
kubectl --context <ctx> -n hive-dev create secret generic hive-db --from-literal=DB_PASSWORD='...'

# 2. Deploy (helm upgrade --install --atomic --wait)
make deploy ENV=dev  KUBE_CONTEXT=<ctx>                  # namespace hive-dev
make deploy ENV=prod KUBE_CONTEXT=<ctx> NAMESPACE=hive
make deploy ENV=prod KUBE_CONTEXT=<ctx> HELM_ARGS="--set image.tag=<sha>"

make helm-lint                 # lint every env
make helm-template ENV=prod    # render manifests
```

Notes:
- Images: `417732881703.dkr.ecr.eu-west-2.amazonaws.com/health-app:<short commit sha>`.
  The image is ARM64-only, so the cluster needs ARM nodes. A non-EKS cluster needs
  `imagePullSecrets` to pull from ECR.
- On an empty database do the first prod deploy with one replica (see
  `helm/envs/prod/values.yaml`).
- `preStopSleepSeconds` needs Kubernetes 1.30+; set it to `0` on older clusters.
