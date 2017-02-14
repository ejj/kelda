var wordpress = require("github.com/NetSys/quilt/specs/wordpress/wordpress");
var memcached = require("github.com/NetSys/quilt/specs/memcached/memcached");
var mysql = require("github.com/NetSys/quilt/specs/mysql/mysql");
var haproxy = require("github.com/quilt/haproxy");
var spark = require("github.com/NetSys/quilt/specs/spark/spark");

var memcd = new memcached.Memcached(3);
var db = new mysql.Mysql(2);
var sprk = new spark.Spark(1, 4); // 1 Master, 4 Workers
var wp = new wordpress.Wordpress(4, db, memcd);
var hap = new haproxy.Haproxy(2, wp.wp);

sprk.workers.connect(7077, db.master);
hap.public();

// Infrastructure
var deployment = createDeployment({
    namespace: "CHANGE_ME",
    adminACL: ["local"],
});

var nWorker = 4;
var baseMachine = new Machine({
    provider: "Amazon",
    region: "us-west-1",
    size: "m4.large",
    sshKeys: githubKeys("ejj"), // Replace with your GitHub username.
});

deployment.deploy(baseMachine.asMaster())
deployment.deploy(baseMachine.asWorker().replicate(nWorker + 1))
deployment.deploy([memcd, db, sprk, wp, hap]);
