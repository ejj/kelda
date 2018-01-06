# Kelda-Kube
This is a development fork for the transition from our custom scheduler to
Kubernetes.

## Repo Maintenance

**No branches are safe from force-pushing**. Only Kevin will force push.

### master
`master` will track the master branch of `kelda/kelda`.

### kube-stage
The `kube-stage` branch will track the "clean" version of the code. Only Kevin
will manage this branch. The goal of this branch is to have a working copy of
the Kubernetes implementation that we can always refer back to for checkpoints,
and as a way to organize commits that are ready to merge into the official
repo.

Every once in awhile, Kevin will merge commits from the `kube-dev` branch into this
branch. He might squash them to make the history cleaner. Commits that are ready
to be merged into the official repo will be based off this branch. Once merged,
this branch will be rebased against `master` of `kelda/kelda`.

### kube-dev
The `kube-dev` branch will be the shared development branch for both Kevin and Ethan.
This branch will be committed to much more frequently, maybe once a day. The
purpose of this branch is so that the code that Kevin and Ethan develop on
doesn't diverge too much. Once a set of commits have been moved to `kube-stage`,
Kevin will rebase this branch.

## TODOs

### Feature Complete
Missing features supported by the current scheduler implementation:
- Placement
- Container.filepathToContent
- Support environment variable values other than String
  - Secret
  - RuntimeInfo
- Fix `kelda debug-logs`

### Network
- Implement inter-container communication
- Implement DNS
- Implement inbound/outbound public traffic
- Implement load balancing (or rip it out)

### Kubernetes Robustness
Necessary Kubernetes improvements to the Kubernetes deployment to make it
reasonable to run in production.
- Use HTTPS between the kube-apiserver and kubelets
- Authenticate and authorize requests to the kube-apiserver
- Only allow insecure connection to the kube-apiserver via localhost
- Test failover
  - Containers should continue running if the leader dies
  - If a container dies, Kube should restart it
  - If a worker dies, containers should be rescheduled
- Look into delay from containers booting to them showing up in `kelda show`
- Fix log message warnings in kubelet
