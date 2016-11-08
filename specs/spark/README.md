# Quilt: Spark
This document describes how to run Apache Spark on Quilt and pass it a job at
boot time. Specifically, we'll be running the SparkPI example to calculate the
value of π on our Quilt Spark cluster.

## Configuring QUILT_PATH
Quilt uses the `QUILT_PATH` environment variable to locate packages. See the
[Stitch language spec](../../docs/Stitch.md#quilt_path) for instructions on
setting up your `QUILT_PATH`.

To fetch the specs that come with `quilt`, execute `quilt get github.com/NetSys/quilt`.

## SparkPi
The example SparkPi program distributes the computationally-intensive task of
calculating Pi over several machines in a computer cluster.

Our [sparkPI.spec](sparkPI.spec) Stitch specification simplifies the
task of setting up the infrastructure required to run this Spark job.

### Configure SSH authentication
Quilt-managed Machines use public key authentication to control SSH access.
To read the result of the Spark job, we will need to access the Master VM.

If you would like to use `githubKey` authentication, open
`specs/spark/sparkPI.spec` and fill in
`(define githubKey "<YOUR_GITHUB_USERNAME>")` appropriately.

For instructions on configuring a user-supplied public key and more information
on configuring Quilt SSH authentication, see
[GettingStarted.md](../../docs/GettingStarted.md#set-up-your-ssh-authentication).

### Choose Namespace
Running two Quilt instances with the same Namespace is not supported.
If you are sharing a computing cluster with others, it would be a good idea to
change `(define Namespace "CHANGE_ME")` to a different name.

### Build `sparkPI.spec`
Start the Quilt daemon with `quilt daemon`. Then, in a separate shell, execute
`quilt run github.com/NetSys/quilt/specs/spark/sparkPI.spec` to
build this Stitch specification.

Quilt will now begin provisioning several VMs on your cloud provider. Five VMs
will serve as Workers, and one will be the Master.

It will take a bit for the VMs to boot up for Quilt to configure the network,
and for Docker containers to be initialized. The following output reports that
the Master's public IP is `54.183.162.8`:
```
INFO [Jun  8 10:41:09.268] Successfully booted machines.
INFO [Jun  8 10:41:20.820] db.Machine:
    Machine-2{Role=Master, Provider=Amazon, Region=us-west-1, Size=m4.2xlarge, DiskSize=32, CloudID=sir-041ecz1b, PublicIP=54.183.162.8, PrivateIP=172.31.11.161}
    Machine-8{Role=Worker, Provider=Amazon, Region=us-west-1, Size=m4.2xlarge, DiskSize=32, CloudID=sir-041f2tpn, PublicIP=54.67.99.218, PrivateIP=172.31.15.97}

[truncated]
```

A "New connection" message in the console output indicates that new VM is fully
booted and has began communicating with Quilt:

```
INFO [Jun  8 10:44:10.523] New connection.
    machine=Machine-2{Role=Master, Provider=Amazon, Region=us-west-1, Size=m4.2xlarge, DiskSize=32, CloudID=sir-041ecz1b, PublicIP=54.183.162.8, PrivateIP=172.31.11.161}
```

Once you see the "New connection" message, you can connect to the Machines with the command
`quilt ssh <MACHINE_NUM>`, or manually with `ssh quilt@<PUBLIC_IP>`.

### Inspect Containers
To list all active containers in the cluster, use `quilt containers`.  For example:
```
$ quilt containers
Container-40{run quilt/spark run worker, Minion: 172.31.3.163, StitchID: 4, IP: 10.0.163.12, Mac: 02:00:0a:00:a3:0c, Labels: [spark-wk-2], Env: map[MASTERS:spark-ms-0.q]}
Container-37{run quilt/spark run master, Minion: 172.31.11.3, StitchID: 1, IP: 10.0.155.142, Mac: 02:00:0a:00:9b:8e, Labels: [spark-ms-0], Env: map[JOB:run-example SparkPi]}
Container-38{run quilt/spark run worker, Minion: 172.31.11.189, StitchID: 2, IP: 10.0.9.118, Mac: 02:00:0a:00:09:76, Labels: [spark-wk-0], Env: map[MASTERS:spark-ms-0.q]}
Container-39{run quilt/spark run worker, Minion: 172.31.11.205, StitchID: 3, IP: 10.0.11.2, Mac: 02:00:0a:00:0b:02, Labels: [spark-wk-1], Env: map[MASTERS:spark-ms-0.q]}
```

### Recovering Pi
Once our Master Spark container is up, we can find the results of our SparkPi job via
logs.

Execute `quilt logs <Stitch ID>`. After scrolling through Spark's info logging, we will
find the result of SparkPi:

```
$ quilt logs 1
# ...
16/06/08 18:49:42 INFO TaskSchedulerImpl: Removed TaskSet 0.0, whose tasks have all completed, from pool
16/06/08 18:49:42 INFO DAGScheduler: ResultStage 0 (reduce at SparkPi.scala:36) finished in 0.381 s
16/06/08 18:49:42 INFO DAGScheduler: Job 0 finished: reduce at SparkPi.scala:36, took 0.525937 s
Pi is roughly 3.13918
16/06/08 18:49:42 INFO SparkUI: Stopped Spark web UI at http://10.0.254.144:4040
16/06/08 18:49:42 INFO MapOutputTrackerMasterEndpoint: MapOutputTrackerMasterEndpoint stopped!
16/06/08 18:49:42 INFO MemoryStore: MemoryStore cleared
16/06/08 18:49:42 INFO BlockManager: BlockManager stopped
```

**Note:** The Spark cluster is now up and usable. You can run the interactive
spark-shell by exec-ing it in the Master Spark container:
`quilt exec <MASTER_CONTAINER_ID> spark-shell`
