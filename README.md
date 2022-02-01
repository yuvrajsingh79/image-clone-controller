# `Image Clone Controller`

The `Image Clone Controller` watches for Deployments/DaemonSets and checks if images used are not from
registry "backupregistry" then pulls the image, tags it and pushes to "backupregistry"

Controller is written in Go

* Watch the Kubernetes Deployment and DaemonSet objects
* Check if any of them provision pods with images that are not from the backup
registry
* If yes, copy the image over to a corresponding repository and tag in the backup
registry
* Modify the Deployment/DaemonSet to use the image from the backup registry
* IMPORTANT: The Deployments and DaemonSets in the kube-system namespace
is ignored!


Registry secret yaml should be present

Create the role, role binding, and service account to grant resource permissions to the Operator, and Image Clone Operator:
```
$ kubectl create -f yaml/controller/controller-secret.yaml
$ kubectl create -f yaml/rbac
$ kubectl create -f yaml/controller/controller-dep.yaml
```