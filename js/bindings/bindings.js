/* eslint no-underscore-dangle: "off" */
const crypto = require('crypto');
const request = require('sync-request');
const stringify = require('json-stable-stringify');
const _ = require('underscore');
const path = require('path');
const os = require('os');

// Use `let` to enable mocking of `fs` in tests.
let fs = require('fs'); // eslint-disable-line prefer-const

const googleDescriptions = require('./googleDescriptions');
const amazonDescriptions = require('./amazonDescriptions');
const digitalOceanDescriptions = require('./digitalOceanDescriptions');

const providerDefaultRegions = {
  Amazon: 'us-west-1',
  Google: 'us-east1-b',
  DigitalOcean: 'sfo1',
  Vagrant: '',
};

const githubCache = {};
const objectHasKey = Object.prototype.hasOwnProperty;

// The default Infrastructure object. The Infrastructure constructor overwrites
// this.
let _keldaInfrastructure;

class Infrastructure {
  /**
   * Creates a new Infrastructure with the given options.
   * @constructor
   *
   * @param {Machine|Machine[]} masters - One or more machines that should be launched to
   *   use as the masters.
   * @param {Machine|Machine[]} workers - One or more machines that should be launched to
   *   use as the workers.  Worker machines are responsible for running application containers.
   * @param {Object} [opts] - Optional arguments to tweak the behavior
   *   of the namespace.
   * @param {string} [opts.namespace=kelda] - The name of the
   *   namespace that the blueprint should operate in.
   * @param {string[]} [opts.adminACL] - A list of IP addresses that are
   *   allowed to access the deployed machines.  The IP of the machine where the
   *   daemon is running is always allowed to access the machines. If you would like to allow
   *   another machine to access the deployed machines (e.g., to SSH into a machine),
   *   add its IP address here.  These IP addresses must be in CIDR notation; e.g.,
   *   to allow access from 1.2.3.4, set adminACL to ["1.2.3.4/32"]. To allow access
   *   from all IP addresses, set adminACL to ["0.0.0.0/0"].
   */
  constructor(masters, workers, opts = {}) {
    this.namespace = opts.namespace || 'kelda';
    this.adminACL = getStringArray('adminACL', opts.adminACL);

    checkExtraKeys(opts, this);

    this.machines = [];
    this.containers = new Set();
    this.loadBalancers = [];

    const boxedMasters = boxObjects(masters, Machine);
    const boxedWorkers = boxObjects(workers, Machine);
    if (boxedMasters.length < 1) {
      throw new Error('masters must include 1 or more Machines to use as ' +
        'Kelda masters.');
    } else if (boxedWorkers.length < 1) {
      throw new Error('workers must include 1 or more Machines to use as ' +
        'Kelda workers.');
    }

    const machineWithRole = (machine, role) => {
      const copy = machine.clone();
      copy.role = role;
      return copy;
    };

    boxedMasters.forEach(master =>
      this.machines.push(machineWithRole(master, 'Master')));
    boxedWorkers.forEach(worker =>
      this.machines.push(machineWithRole(worker, 'Worker')));

    if (_keldaInfrastructure !== undefined) {
      throw new Error('the Infrastructure constructor has already been called once ' +
        '(each Kelda blueprint can only define one Infrastructure).');
    }
    _keldaInfrastructure = this;
  }

  /**
   * Converts the infrastructure to the JSON format expected by the Kelda go
   * code.
   * @private
   * @returns {Object} A map that can be converted to JSON and interpreted by the Kelda
   *   Go code.
   */
  toKeldaRepresentation() {
    setKeldaIDs(this.containers);

    const loadBalancers = [];
    let connections = [];
    let placements = [];
    const containers = [];

    // Convert the load balancers.
    this.loadBalancers.forEach((lb) => {
      connections = connections.concat(lb.getKeldaConnections());
      loadBalancers.push({
        name: lb.name,
        hostnames: lb.containers.map(c => c.hostname),
      });
    });

    this.containers.forEach((c) => {
      connections = connections.concat(c.getKeldaConnections());
      placements = placements.concat(c.getPlacementsWithID());
      containers.push(c.toKeldaRepresentation());
    });

    const machines = this.machines.map(m => m.toKeldaRepresentation());

    const keldaInfrastructure = {
      machines,
      loadBalancers,
      containers,
      connections,
      placements,

      namespace: this.namespace,
      adminACL: this.adminACL,
    };
    vet(keldaInfrastructure);
    return keldaInfrastructure;
  }
}

/**
 * Gets the public key associated with a github username.
 * @param {string} user - The GitHub username.
 * @returns {string} The SSH key.
 */
function githubKeys(user) {
  if (user in githubCache) {
    return githubCache[user];
  }

  const response = request('GET', `https://github.com/${user}.keys`);
  if (response.statusCode >= 300) {
    // Handle any errors.
    throw new Error(
      `HTTP request for ${user}'s github keys failed with error ` +
            `${response.statusCode}`);
  }

  const keys = response.getBody('utf8').trim().split('\n');
  githubCache[user] = keys;

  return keys;
}

// Both infraDirectory and baseInfraLocation are also defined in initializer/constants.js.
// This code duplication is ugly, but it significantly simplifies packaging
// the `kelda init` code with the "@kelda/install" module.
const infraDirectory = path.join(os.homedir(), '.kelda', 'infra');
const baseInfraLocation = path.join(infraDirectory, 'default.js');

/**
 * Returns the Infrastructure object exported by the base infrastructure
 * blueprint.
 * Having this as a separate function simplifies testing baseInfrastructure().
 * @private
 *
 * @returns {Infrastructure} - The Infrastructure exported by the infrastructure
 *  blueprint.
 */
function getBaseInfrastructure() {
  const infraGetter = require(baseInfraLocation); // eslint-disable-line

  // By passing this module to the infraGetter, the blueprint doesn't have to
  // require Kelda directly and we thus don't have to `npm install` in the
  // infrastructure directory, which would be messy.
  return infraGetter(module.exports);
}

/**
 * Returns a base infrastructure. The base infrastructure could be created
 * with `kelda init`.
 *
 * @example <caption>Retrieve the base infrastructure, and deploy
 * an nginx container on it.</caption>
 * const inf = baseInfrastructure();
 * (new Container('web', 'nginx')).deploy(inf);
 *
 * @returns {Infrastructure} The infrastructure object.
 */
function baseInfrastructure() {
  if (!fs.existsSync(baseInfraLocation)) {
    throw new Error('no base infrastructure. Use `kelda init` to create one.');
  }
  return getBaseInfrastructure();
}

// The name used to refer to the public internet in the JSON description
// of the network connections (connections to other entities are referenced by
// hostname, but since the public internet is not a container or load balancer,
// we need a special label for it).
const publicInternetLabel = 'public';

// Global unique ID counter.
let uniqueIDCounter = 0;

/**
 * @private
 * @returns {integer} A globally unique integer ID.
 */
function uniqueID() {
  uniqueIDCounter += 1;
  return uniqueIDCounter;
}

/**
 * Deterministically sets the id field of objects based on their attributes. The
 * _refID field is required to differentiate between multiple references to the
 * same object, and multiple instantiations with the exact same attributes.
 * @private
 *
 * @param {Object[]} objs - An array of objects.
 * @returns {void}
 */
function setKeldaIDs(objs) {
  // The refIDs for each identical instance.
  const refIDs = {};
  objs.forEach((obj) => {
    const k = obj.hash();
    if (!refIDs[k]) {
      refIDs[k] = [];
    }
    refIDs[k].push(obj._refID);
  });

  // If there are multiple references to the same object, there will be
  // duplicate refIDs.
  Object.keys(refIDs).forEach((k) => {
    refIDs[k] = _.sortBy(_.uniq(refIDs[k]), _.identity);
  });

  objs.forEach((obj) => {
    const k = obj.hash();
    const h = hash(k + refIDs[k].indexOf(obj._refID));
    obj.id = h; // eslint-disable-line no-param-reassign
  });
}

/**
 * Cryptographically hashes the given string.
 * @private
 *
 * @param {string} str - The string to be hashed.
 * @returns {string} The hash.
 */
function hash(str) {
  const shaSum = crypto.createHash('sha1');
  shaSum.update(str);
  return shaSum.digest('hex');
}

/**
 * Checks if the namespace is lower case, and if all referenced
 * containers in connections and load balancers are really deployed.
 * @private
 *
 * @param {Infrastructure} infrastructure - An infrastructure object.
 * @returns {void}
 */
function vet(infrastructure) {
  if (infrastructure.namespace !== infrastructure.namespace.toLowerCase()) {
    throw new Error(`namespace "${infrastructure.namespace}" contains ` +
                  'uppercase letters. Namespaces must be lowercase.');
  }
  const lbHostnames = infrastructure.loadBalancers.map(l => l.name);
  const containerHostnames = infrastructure.containers.map(c => c.hostname);
  const hostnames = lbHostnames.concat(containerHostnames);

  const hostnameMap = { [publicInternetLabel]: true };
  hostnames.forEach((hostname) => {
    if (hostnameMap[hostname] !== undefined) {
      throw new Error(`hostname "${hostname}" used multiple times`);
    }
    hostnameMap[hostname] = true;
  });

  infrastructure.connections.forEach((conn) => {
    conn.from.concat(conn.to).forEach((host) => {
      if (!hostnameMap[host]) {
        throw new Error(`connection ${stringify(conn)} references ` +
                    `an undefined hostname: ${host}`);
      }
    });
  });

  const dockerfiles = {};
  infrastructure.containers.forEach((c) => {
    const name = c.image.name;
    if (dockerfiles[name] !== undefined &&
                dockerfiles[name] !== c.image.dockerfile) {
      throw new Error(`${name} has differing Dockerfiles`);
    }
    dockerfiles[name] = c.image.dockerfile;
  });

  // Check to make sure all machines have the same region and provider.
  let lastMachine;
  infrastructure.machines.forEach((m) => {
    if (lastMachine !== undefined &&
      (lastMachine.region !== m.region || lastMachine.provider !== m.provider)) {
      throw new Error('All machines must have the same provider and region. '
        + `Found providers '${lastMachine.provider}' in region '${lastMachine.region}' `
        + `and '${m.provider}' in region '${m.region}'.`);
    }
    lastMachine = m;
  });
}


class LoadBalancer {
  /**
   * Creates a new LoadBalancer object which represents a collection of
   * containers behind a load balancer.
   * @implements {Connectable}
   * @constructor
   *
   * @param {string} name - The name of the load balancer.
   * @param {Container[]} containers - The containers behind the load balancer.
   */
  constructor(name, containers) {
    if (typeof name !== 'string') {
      throw new Error(`name must be a string; was ${stringify(name)}`);
    }
    this.name = uniqueHostname(name);
    this.containers = boxObjects(containers, Container);

    this.allowedInboundConnections = [];
  }

  /**
   * @returns {string} The Kelda hostname that represents the entire load balancer.
   */
  hostname() {
    return `${this.name}.q`;
  }

  /**
   * Adds this load balancer to the given infrastructure.
   *
   * @param {Infrastructure} infrastructure - The Infrastructure that this
   *  should be added to.
   * @returns {void}
   */
  deploy(infrastructure) {
    infrastructure.loadBalancers.push(this);
  }

  /**
   * Allows inbound connections to the load balancer. Note that this does not
   * allow direct connections to the containers behind the load balancer.
   *
   * @param {Container|Container[]} srcArg - The containers that can open
   *   connections to this load balancer.
   * @param {int|Port|PortRange} portRange - The ports on which containers can
   *   open connections.
   * @returns {void}
   */
  allowFrom(srcArg, portRange) {
    let src;
    try {
      src = boxObjects(srcArg, Container);
    } catch (err) {
      throw new Error('Load Balancers can only allow traffic from containers. ' +
            'Check that you\'re allowing connections from a Container ' +
            'or list of containers and not from a Load Balancer or other object.');
    }

    this.allowedInboundConnections.push(
      new Connection(src, boxRange(portRange)));
  }

  /**
   * @private
   * @returns {Object} - A list of maps describing the inbound connections to the load balancer, in
   *   a format that can be converted to JSON and sent to the Kelda Go code.
   */
  getKeldaConnections() {
    return this.allowedInboundConnections.map(conn => ({
      from: conn.from.map(f => f.hostname),
      to: [this.name],
      minPort: conn.minPort,
      maxPort: conn.maxPort,
    }));
  }
}

// publicInternet is an object that looks like another container that can
// allow inbound connections. However, it is actually just syntactic sugar
// to hide the allowOutboundPublic and allowFromPublic functions.
/**
 * @implements {Connectable}
 */
const publicInternet = {
  /**
   * @param {Container|Container[]} srcArg - A Container or list of Containers
   *   that should be allowed to connect to the public internet.
   * @param {number|Range|PortRange} portRange - A port or range of ports that the
   *   given container(s) are allowed to connect to the public internet on.
   * @returns {void}
   */
  allowFrom(srcArg, portRange) {
    let src;
    try {
      src = boxObjects(srcArg, Container);
    } catch (err) {
      throw new Error('Only containers can connect to public. ' +
                'Check that you\'re allowing connections from a Container or ' +
                'list of containers and not from a Load Balancer or other object.');
    }

    src.forEach((c) => {
      c.allowOutboundPublic(portRange);
    });
  },
};

/**
 * Boxes an object into a list of objects, or does nothing if `x` is already
 * a list of objects. Also checks that all items in the list are instances
 * of `type`. This method is useful for validating arguments to functions.
 * @private
 *
 * @param {Object|Object[]} x - An object or list of objects.
 * @param {Object} type - A constructor (used to check whether `x` was constructed
 *   using this constructor).
 * @returns {Object[]} The resulting list of objects.
 */
function boxObjects(x, type) {
  if (x instanceof type) {
    return [x];
  }

  assertArrayOfType(x, type);
  return x;
}

/**
 * Checks that `array` is an array of `type` objects, and throws an
 * error if it is not.
 * @private
 *
 * @param {Object[]} array - An array of objects to check the type of.
 * @param {Object} type - The constructor to check that all items in `array`
 *   are types of.
 * @returns {void}
 */
function assertArrayOfType(array, type) {
  if (!Array.isArray(array)) {
    throw new Error(`not an array of ${type.name}s (was ` +
            `${stringify(array)})`);
  }
  for (let i = 0; i < array.length; i += 1) {
    if (!(array[i] instanceof type)) {
      throw new Error(`not an array of ${type.name}s; item ` +
                `at index ${i} (${stringify(array[i])}) is not a ` +
                `${type.name}`);
    }
  }
}

let hostnameCount = {};

/**
 * @private
 * @param {string} name - The name that the generated hostname should be based
 *   on.
 * @returns {string} The unique hostname.
 */
function uniqueHostname(name) {
  if (!(name in hostnameCount)) {
    hostnameCount[name] = 1;
    return name;
  }
  hostnameCount[name] += 1;
  return uniqueHostname(name + hostnameCount[name]);
}

/**
 * Boxes raw integers into range.
 * @private
 *
 * @param {integer|Range} x - The integer to be boxed into the range (or
 *   undefined).
 * @returns {Range} The resulting Range object.
 */
function boxRange(x) {
  if (x === undefined) {
    return new Range(0, 0);
  }
  if (typeof x === 'number') {
    return new Range(x, x);
  }
  if (!(x instanceof Range)) {
    throw new Error('Input argument must be a number or a Range');
  }
  return x;
}

/**
  * Throws an error if the first object contains any keys that do not appear in
  * the second object.
  * This function is useful for checking if the user passed invalid options to
  * functions that take optional arguments. Namely, since all valid user given
  * optional arguments are added as properties of the new object, any key
  * that appears in the optional argument but not as a property of the object
  * must be an unexpected optional argument.
  * @private
  *
  * @param {Object} opts - The Object to check for redundant keys.
  * @param {Object} obj - The object to compare against.
  * @returns {void}
  * @throws Throws an error if redundant keys are found in `opts`.
  */
function checkExtraKeys(opts, obj) {
  // Sometimes, prototype constructors are called internally by Kelda. In these
  // cases, an existing object is passed as the optional argument, and the
  // optional argument thus contains not just the keys passed by the user, but
  // also the keys Kelda set on the object, as well as all the prototype
  // methods. Since we only want to check the optional arguments passed by the
  // user, we skip all calls internally from Kelda (indicated by having the
  // refID set in the options).
  if (objectHasKey.call(opts, '_refID')) { return; }

  const extras = Object.keys(opts).filter(key => !objectHasKey.call(obj, key));

  if (extras.length > 0) {
    throw new Error('Unrecognized keys passed to ' +
      `${obj.constructor.name} constructor: ${extras}`);
  }
}

/**
 * Forces `arg` to be a number, even if it's undefined.
 * @private
 *
 * @param {string} argName - The name of the number (for logging).
 * @param {number} arg - The number that might be undefined.
 * @returns {number} Zero if `arg` is not defined, and otherwise ensures that
 *   `arg` is a number and then returns it.
 */
function getNumber(argName, arg) {
  if (arg === undefined) {
    return 0;
  }
  if (typeof arg === 'number') {
    return arg;
  }
  throw new Error(`${argName} must be a number (was: ${stringify(arg)})`);
}

/**
 * Forces `arg` to be a string, even if it's undefined.
 * @private
 *
 * @param {string} argName - The name of the string (for logging).
 * @param {string} arg - The arg that might be undefined.
 * @returns {string} An empty string if `arg` is not defined, and otherwise
 *   ensures that `arg` is a string and then returns it.
 */
function getString(argName, arg) {
  if (arg === undefined) {
    return '';
  }
  if (typeof arg === 'string') {
    return arg;
  }
  throw new Error(`${argName} must be a string (was: ${stringify(arg)})`);
}

/**
 * @private
 * @param {string} argName - The name of `arg` (for logging).
 * @param {Object.<string, string|Secret|RuntimeValue>} arg - The map of
 *   strings to Secret, RuntimeValue, or Strings.
 * @returns {Object.<string, string|Secret|RuntimeValue>} An empty object
 *   if `arg` is not defined, and otherwise ensures that `arg` is an object with
 *   string keys and string, RuntimeValue, or Secret values and then returns it.
 */
function getSecretOrStringMap(argName, arg) {
  if (arg === undefined) {
    return {};
  }
  if (typeof arg !== 'object') {
    throw new Error(`${argName} must be a map ` +
            `(was: ${stringify(arg)})`);
  }
  Object.keys(arg).forEach((k) => {
    if (typeof k !== 'string') {
      throw new Error(`${argName} must be a map with string keys (key ` +
                `${stringify(k)} is not a string)`);
    }
    const val = arg[k];
    if (typeof val !== 'string' && !(val instanceof Secret) &&
      !(val instanceof RuntimeValue)) {
      throw new Error(`${argName} must be a map with string, RuntimeValue, ` +
        `or Secret values (value ${stringify(arg[k])} associated ` +
        `with ${k} is not a string, RuntimeValue or Secret)`);
    }
  });
  return arg;
}

/**
 * Verifies `arg` is an array of strings or undefined.
 * @private
 *
 * @param {string} argName - The name of `arg` (for logging).
 * @param {string[]} arg - The array of strings.
 * @returns {string[]} Returns an empty array if `arg` is not
 *   defined, and otherwise ensures that `arg` is an array of strings and then
 *   returns it.
 */
function getStringArray(argName, arg) {
  if (arg === undefined) {
    return [];
  }
  if (!Array.isArray(arg)) {
    throw new Error(`${argName} must be an array of strings ` +
            `(was: ${stringify(arg)})`);
  }
  for (let i = 0; i < arg.length; i += 1) {
    if (typeof arg[i] !== 'string') {
      throw new Error(`${argName} must be an array of strings. ` +
                `Item at index ${i} (${stringify(arg[i])}) is not a ` +
                'string.');
    }
  }
  return arg;
}

/**
 * @private
 * @param {string} argName - The name of `arg` (for logging).
 * @param {boolean} arg - The boolean that might be undefined.
 * @returns {boolean} False if `arg` is not defined, and otherwise ensures
 *   that `arg` is a boolean and then returns it.
 */
function getBoolean(argName, arg) {
  if (arg === undefined) {
    return false;
  }
  if (typeof arg === 'boolean') {
    return arg;
  }
  throw new Error(`${argName} must be a boolean (was: ${stringify(arg)})`);
}

class Machine {
  /**
   * Creates a new Machine object, which represents a machine to be deployed.
   * The constructor will set the Machine's size, region, cpu, and ram properties
   * based on the cloud virtual machine that will be launched for this Machine.
   * @constructor
   *
   * @example <caption>Create a Machine on Amazon. This will use the
   * default size and region for Amazon.</caption>
   * const baseMachine = new Machine({provider: 'Amazon'});
   *
   * @example <caption>Create a machine with the 'n1-standard-1' size in
   * GCE's 'us-east1-b' region.</caption>
   * const googleWorker = new Machine({
   *   provider: 'Google',
   *   region: 'us-east1-b',
   *   size: 'n1-standard-1',
   * });
   *
   * @example <caption>Create a DigitalOcean droplet with the '512mb' size
   * in the 'sfo1' zone.</caption>
   * const googleWorker = new Machine({
   *   provider: 'DigitalOcean',
   *   region: 'sfo1',
   *   size: '512mb',
   * });
   *
   * @param {Object.<string, string>} opts - Arguments that modify the machine.
   *   Only 'provider' is required; the remaining options are optional.
   * @param {string} opts.provider - The cloud provider that the machine
   *   should be launched in. Accepted values are Amazon, DigitalOcean, Google,
   *   and Vagrant.
   * @param {string} [opts.region] - The region the machine will run-in
   *   (provider-specific; e.g., for Amazon, this could be 'us-west-2').
   * @param {string} [opts.size] - The instance type (provider-specific).
   * @param {Range|int} [opts.cpu] - The desired number of CPUs.
   * @param {Range|int} [opts.ram] - The desired amount of RAM in GiB.
   * @param {int} [opts.diskSize] - The desired amount of disk space in GB.
   * @param {string} [opts.floatingIp] - A reserved IP to associate with
   *   the machine.
   * @param {string[]} [opts.sshKeys] - Public keys to allow users to log
   *   in to the machine and containers running on it.
   * @param {boolean} [opts.preemptible=false] - Whether the machine
   *   should be preemptible. Only supported on the Amazon provider.
   */
  constructor(opts) {
    this._refID = uniqueID();

    this.provider = getString('provider', opts.provider);
    if (this.provider === '') {
      throw new Error('Machine must specify a provider (accepted values are Amazon, ' +
        'DigitalOcean, Google, and Vagrant');
    }
    this.role = getString('role', opts.role);
    this.region = getString('region', opts.region);
    this.size = getString('size', opts.size);
    this.floatingIp = getString('floatingIp', opts.floatingIp);
    this.diskSize = getNumber('diskSize', opts.diskSize);
    this.sshKeys = getStringArray('sshKeys', opts.sshKeys);
    this.preemptible = getBoolean('preemptible', opts.preemptible);

    this.chooseSize(boxRange(opts.cpu), boxRange(opts.ram));
    this.chooseRegion();

    // Check for extra keys after calling chooseSize, which sets the machine size,
    // CPU, and RAM.
    checkExtraKeys(opts, this);
  }

  /**
   * If size is not specified, sets the machine's size attribute to an instance
   * size (e.g., m2.xlarge), based on the Machine's specified provider, region,
   * and hardware. Throws an error if provider is not valid. If size is specified,
   * verifies the size is valid for the given provider and meets the CPU and RAM
   * requirements.
   * @private
   * @param {Range} cpu - The desired number of CPUs.
   * @param {Range} ram - The desired amount of RAM in GiB.
   * @returns {void}
   */
  chooseSize(cpu, ram) {
    if (this.provider === 'Vagrant') {
      this.vagrantSize(cpu, ram);
      return;
    }
    let providerDescriptions;
    switch (this.provider) {
      case 'Amazon':
        providerDescriptions = amazonDescriptions.Descriptions;
        break;
      case 'DigitalOcean':
        providerDescriptions = digitalOceanDescriptions.Descriptions;
        break;
      case 'Google':
        providerDescriptions = googleDescriptions.Descriptions;
        break;
      default:
        throw new Error(`Unknown Cloud Provider: ${this.provider}`);
    }
    let machineDescription;
    if (this.size !== '') {
      machineDescription = this.verifySize(providerDescriptions, cpu, ram);
    } else {
      machineDescription = this.chooseBestSize(providerDescriptions, cpu, ram);
    }

    // Set the machine's attributes based on the description of the cloud provider
    // VM that will be launched.
    this.size = machineDescription.Size;
    this.ram = machineDescription.RAM;
    this.cpu = machineDescription.CPU;
  }

  /**
   * Verifies that user-requested machine size is valid for the given provider.
   * If so, verifies the requested machine size satisfies CPU and RAM requirements,
   * and returns the description of the machine that the provider will launch for
   * the specified machine size.
   * @private
   * @param {description[]} providerDescriptions - Array of descriptions of
   *   a provider.
   * @param {Range} cpu - The desired number of CPUs.
   * @param {Range} ram - The desired amount of RAM in GiB.
   * @returns {Object} - The description of the machine that will be launched
   *   by the cloud provider.
   */
  verifySize(providerDescriptions, cpu, ram) {
    for (let i = 0; i < providerDescriptions.length; i += 1) {
      const description = providerDescriptions[i];
      if (this.size !== '' && this.size === description.Size) {
        if (ram.inRange(description.RAM) &&
            cpu.inRange(description.CPU)) {
          return description;
        }
        throw new Error(`Requested size '${this.size}' does not meet`
          + ` RAM '${ram}' or`
          + ` CPU '${cpu}' requirements.`
          + ` Instance RAM: '${description.RAM}',`
          + ` Instance CPU: '${description.CPU}'`);
      }
    }
    throw new Error(`Invalid machine size "${this.size}"`
      + ` for provider ${this.provider}`);
  }

  /**
   * Sets the machine's region using the default region of the specified provider
   * Throws an error if provider is not valid or given machine requirements cannot
   * be satisfied by any size.
   * @private
   * @returns {void}
   */
  chooseRegion() {
    if (this.region !== '') return;
    if (this.provider in providerDefaultRegions) {
      this.region = providerDefaultRegions[this.provider];
    } else {
      throw new Error(`Unknown Cloud Provider: ${this.provider}`);
    }
  }

  /**
   * Iterates through all the decriptions for a given provider, and returns
   * the cheapest option that fits the user's requirements. Throws an error if given
   * machine requirements cannot be satisfied by any size.
   * @private
   * @param {description[]} providerDescriptions - Array of descriptions of
   *   a provider.
   * @param {Range} cpu - The desired number of CPUs.
   * @param {Range} ram - The desired amount of RAM in GiB.
   * @returns {Object} A description of the best size that fits the user's requirements if
   *   provider is available in Kelda, otherwise throws an error.
   */
  chooseBestSize(providerDescriptions, cpu, ram) {
    let bestMachine;
    for (let i = 0; i < providerDescriptions.length; i += 1) {
      const description = providerDescriptions[i];

      if (description.IgnoredByKelda) {
        continue;
      }

      const isValid = ram.inRange(description.RAM) &&
        cpu.inRange(description.CPU);
      if (!isValid) {
        continue;
      }

      if (bestMachine === undefined || description.Price < bestMachine.Price) {
        bestMachine = description;
      }
    }
    if (bestMachine === undefined) {
      throw new Error(`No valid size for Machine ${stringify(this)}`);
    }
    return bestMachine;
  }

  /**
   * Rounds up RAM and CPU requirements to be at least one for Vagrant.
   * @private
   * @param {Range} cpuRange - The desired number of CPUs.
   * @param {Range} ramRange - The desired amount of RAM in GiB.
   * @returns {string} The rounded up Vagrant size.
   */
  vagrantSize(cpuRange, ramRange) {
    let ram = ramRange.min;
    if (ram < 1) {
      ram = 1;
    }
    let cpu = cpuRange.min;
    if (cpu < 1) {
      cpu = 1;
    }
    this.size = `${ram},${cpu}`;
  }

  /**
   * @returns {Machine} A new machine with the same attributes.
   */
  clone() {
    // _.clone only creates a shallow copy, so we must clone sshKeys ourselves.
    const keyClone = _.clone(this.sshKeys);
    const cloned = _.clone(this);
    cloned.sshKeys = keyClone;
    return new Machine(cloned);
  }

  /**
   * Creates n new machines with the same attributes.
   *
   * @param {number} n - The number of new machines to create.
   * @returns {Machine[]} A list of the new machines. This machine will
   *   not be in the returned list.
   */
  replicate(n) {
    let i;
    const res = [];
    for (i = 0; i < n; i += 1) {
      res.push(this.clone());
    }
    return res;
  }

  /**
   * @private
   * @returns {string} A string describing all attributes of the machine.
   */
  hash() {
    return stringify({
      provider: this.provider,
      role: this.role,
      region: this.region,
      size: this.size,
      floatingIp: this.floatingIp,
      diskSize: this.diskSize,
      preemptible: this.preemptible,
    });
  }

  /**
   * Converts the Machine to the JSON format expected by the Kelda go code.
   * @private
   * @returns {Object} A map that can be converted to JSON and interpreted by the Kelda
   *   Go code.
   */
  toKeldaRepresentation() {
    // Remove the CPU and RAM attributes, which are only included in the Machine object
    // for the user's convenience.
    delete this.cpu;
    delete this.ram;
    return this;
  }
}

class Image {
  /**
   * Creates a Docker Image.
   *
   * If two images with the same name but different Dockerfiles are referenced, an
   * error will be thrown.
   *
   * @constructor
   *
   * @example <caption>Create an image that uses the nginx image stored on
   * Docker Hub.</caption>
   * const image = new Image('nginx');
   *
   * @example <caption>Create an image that uses the etcd image stored at
   * quay.io.</caption>
   * const image = new Image('quay.io/coreos/etcd');
   *
   * @example <caption>Create an Image named my-image-name that's built on top of
   * the nginx image, and additionally includes the Git repository at
   * github.com/my/web/repo cloned into /web_root.</caption>
   * const image = new Image('my-image-name',
   *   'FROM nginx\n' +
   *   'RUN cd /web_root && git clone github.com/my/web_repo');
   *
   * @example <caption>Create an image named my-image-name that's built using a
   * Dockerfile saved locally at 'Dockerfile'.</caption>
   * const fs = require('fs');
   * const container = new Image('my-image-name',
   *   fs.readFileSync('./Dockerfile', { encoding: 'utf8' }));
   *
   * @param {string} name - The name to use for the Docker image, or if no
   *   Dockerfile is specified, the repository to get the image from. The repository
   *   can be a full URL (e.g., quay.io/coreos/etcd) or the name of an image in
   *   Docker Hub (e.g., nginx or nginx:1.13.3).
   * @param {string} [dockerfile] - The string contents of the Dockerfile that
   *   constructs the Image.
   */
  constructor(name, dockerfile) {
    this.name = name;
    this.dockerfile = dockerfile;
  }

  /**
   * @returns {Image} A new Image with all of the same attributes as this Image.
   */
  clone() {
    return new Image(this.name, this.dockerfile);
  }
}

class Container {
  /**
   * Creates a new Container, which represents a container to be deployed.
   *
   * If a Container uses a custom image (e.g., the image is created by reading
   * in a local Dockerfile), Kelda tracks the Dockerfile that was used to create
   * that image.  If the Dockerfile is changed and the blueprint is re-run,
   * the image will be re-built and all containers that use the image will be
   * re-started with the new image.
   *
   * @constructor
   * @implements {Connectable}
   *
   * @example <caption>Create a Container with hostname myApp that uses the nginx
   * image on Docker Hub, and that includes a file located at /etc/myconf with
   * contents foo.</caption>
   * const container = new Container(
   *   'myApp', 'nginx', {filepathToContent: {'/etc/myconf': 'foo'}});
   *
   * @example <caption>Create a Container that has one regular, and one secret
   * environment variable value. The value of `mySecret` must be defined by
   * running `kelda secret mySecret SECRET_VALUE`. If the blueprint with the
   * container is launched before `mySecret` has been added, Kelda will wait to
   * launch the container until the secret's value has been defined.</caption>
   * const container = new Container('myApp', 'nginx', {
   *   env: {
   *     'key1': 'a plaintext value',
   *     'key2': new Secret('mySecret'),
   *   },
   *
   * @example <caption>Create a Container that has its public IP in the
   * SPARK_PUBLIC_DNS environment variable. If the container's public IP changes
   * (e.g. if a floating IP is assigned), the container will be restarted with
   * the new value.</caption>
   * const container = new Container('spark', 'keldaio/spark', {
   *   env: { SPARK_PUBLIC_DNS: hostIP },
   * });
   *
   * @param {string} hostnamePrefix - The network hostname of the container.
   * @param {Image|string} image - An {@link Image} that the container should
   *   boot, or a string with the name of a Docker image (that exists in
   *   Docker Hub) that the container should boot.
   * @param {Object} [opts] - Additional, named, optional arguments.
   * @param {string[]} [opts.command] - The command to use when starting
   *   the container.
   * @param {Object.<string, string|Secret|RuntimeValue>} [opts.env] -
   *   Environment variables to set in the booted container. The key is the name
   *   of the environment variable.
   * @param {Object.<string, string|Secret|RuntimeValue>} [opts.filepathToContent] -
   *   Text files to be installed on the container before it starts.  The key is
   *   the path on the container where the text file should be installed, and
   *   the value is the contents of the text file. If the file content specified
   *   by this argument changes and the blueprint is re-run, Kelda will re-start
   *   the container using the new files.  Files are installed with permissions
   *   0644 and parent directories are automatically created.
   */
  constructor(hostnamePrefix, image, opts = {}) {
    // refID is used to distinguish infrastructures with multiple references to the
    // same container, and infrastructures with multiple containers with the exact
    // same attributes.
    this._refID = uniqueID();

    this.image = image;
    if (typeof image === 'string') {
      this.image = new Image(image);
    }
    if (!(this.image instanceof Image)) {
      throw new Error('image must be an Image or string (was ' +
              `${stringify(image)})`);
    }

    this.hostnamePrefix = getString('hostnamePrefix', hostnamePrefix);
    this.hostname = uniqueHostname(this.hostnamePrefix);
    this.command = getStringArray('command', opts.command);
    this.env = getSecretOrStringMap('env', opts.env);
    this.filepathToContent = getSecretOrStringMap('filepathToContent',
      opts.filepathToContent);

    // Don't allow callers to modify the arguments by reference.
    this.command = _.clone(this.command);
    this.env = _.clone(this.env);
    this.filepathToContent = _.clone(this.filepathToContent);
    this.image = this.image.clone();

    checkExtraKeys(opts, this);

    // When generating the Kelda infrastructure JSON object, these placements must
    // be converted using Container.getPlacementsWithID.
    this.placements = [];

    this.allowedInboundConnections = [];
    this.outgoingPublic = [];
    this.incomingPublic = [];
  }

  /**
   * @returns {Container} A new Container with the same attributes.
   */
  clone() {
    return new Container(this.hostnamePrefix, this.image, this);
  }

  /**
   * Sets the given environment variable to the given value.
   *
   * @param {string} key - The name of the environment variable to set.
   * @param {string} val - The value that the given environment variable
   *   should be given.
   * @returns {void}
   */
  setEnv(key, val) {
    this.env[key] = val;
  }

  /**
   * Creates a new container with the same attributes as this container,
   * except that the environment is set to the given environment.
   *
   * @param {Object.<string, string>} env - A mapping of environment variables
   *   to values for the container.
   * @returns {Container} A new container with the same attributes as this one
   *   except that the environment is set to env.
   */
  withEnv(env) {
    const cloned = this.clone();
    cloned.env = env;
    return cloned;
  }

  /**
   * @returns {string} The container's hostname.
   */
  getHostname() {
    return `${this.hostname}.q`;
  }

  /**
   * @private
   * @returns {string} A string describing all attributes of the machine.
   */
  hash() {
    return stringify({
      image: this.image,
      command: this.command,
      env: this.env,
      filepathToContent: this.filepathToContent,
      hostname: this.hostname,
    });
  }

  /**
   * Sets placement requirements for the Machine that the Container is placed on.
   *
   * @param {Object.<string, string>} machineAttrs - Requirements for the machine the
   *   Container gets placed on.
   * @param {string} [machineAttrs.provider] - Provider that the Container should be placed
   *   in.
   * @param {string} [machineAttrs.size] - Size of the machine that the Container should be placed
   *   on (e.g., m2.4xlarge).
   * @param {string} [machineAttrs.region] - Region that the Container should be placed in.
   * @param {string} [machineAttrs.floatingIp] - Floating IP address that must be assigned to
   *   the machine that the Container gets placed on.
   * @returns {void}
   */
  placeOn(machineAttrs) {
    this.placements.push({
      targetContainer: this.hostname,
      exclusive: false,
      provider: getString('provider', machineAttrs.provider),
      size: getString('size', machineAttrs.size),
      region: getString('region', machineAttrs.region),
      floatingIp: getString('floatingIp', machineAttrs.floatingIp),
    });
  }

  /**
   * Set the targetContainer of the placement rules to be this container. This
   * cannot be done when `placeOn` is called because the container ID is not
   * determined until after all user code has executed.
   * @private
   *
   * @returns {Object} The placements in the form required by the deployment
   *   engine.
   */
  getPlacementsWithID() {
    return this.placements.map((plcm) => {
      plcm.targetContainerID = this.id; // eslint-disable-line no-param-reassign
      return plcm;
    });
  }

  /**
   * Allows connections to this Container from the given Container(s) on the given
   * port or port range.  Containers have a default-deny firewall, meaning that
   * unless connections are explicitly allowed to a container (by calling this
   * function) they will not be allowed.
   *
   * @param {Container|Container[]|publicInternet} srcArg - An entity that should
   *   be allowed to connect to this Container.
   * @param {number|Range|PortRange} portRange - A port or range of ports that the
   *  given Container(s) are allowed to connect to this Container on.
   * @returns {void}
   */
  allowFrom(srcArg, portRange) {
    if (srcArg === publicInternet) {
      this.allowFromPublic(portRange);
      return;
    }

    let src;
    try {
      src = boxObjects(srcArg, Container);
    } catch (err) {
      throw new Error('Containers can only connect to other containers. ' +
              'Check that you\'re allowing connections from a container or ' +
              'list of containers, and not from a LoadBalancer or other object.');
    }

    this.allowedInboundConnections.push(
      new Connection(src, boxRange(portRange)));
  }

  /**
   * Allows outbound connections from this Container to the public internet.
   * Users should access this functionality by calling publicInternet.allowFrom(this, r).
   * @private
   *
   * @param {number|Range} r - A port or port range that this Container should be allowed
   *   to initiate outbound connections to the public internet on.
   * @returns {void}
   */
  allowOutboundPublic(r) {
    const range = boxRange(r);
    if (range.min !== range.max) {
      throw new Error('public internet can only connect to single ports ' +
              'and not to port ranges');
    }
    this.outgoingPublic.push(range);
  }

  /**
   * Allows inbound connections to this Container from the public internet.
   * Users should access this functionality by calling this.allowFrom(publicInternet, r).
   * @private
   *
   * @param {number|Range} r - A port or port range that this Container should accept
   *   inbound connetions from the public internet on.
   * @returns {void}
   */
  allowFromPublic(r) {
    const range = boxRange(r);
    if (range.min !== range.max) {
      throw new Error('public internet can only connect to single ports ' +
              'and not to port ranges');
    }
    this.incomingPublic.push(range);
  }

  /**
   * Adds this Container to be deployed as part of the given infrastructure.
   *
   * @param {Infrastructure} infrastructure - The infrastructure that this should be added to.
   * @returns {void}
   */
  deploy(infrastructure) {
    infrastructure.containers.add(this);
  }

  /**
   * @private
   * @returns {Object} - A list of maps describing the inbound connections to the Container, in
   *   a format that can be converted to JSON and sent to the Kelda Go code.
   */
  getKeldaConnections() {
    const connections = [];

    this.allowedInboundConnections.forEach((conn) => {
      connections.push({
        from: conn.from.map(f => f.hostname),
        to: [this.hostname],
        minPort: conn.minPort,
        maxPort: conn.maxPort,
      });
    });

    this.outgoingPublic.forEach((rng) => {
      connections.push({
        from: [this.hostname],
        to: [publicInternetLabel],
        minPort: rng.min,
        maxPort: rng.max,
      });
    });

    this.incomingPublic.forEach((rng) => {
      connections.push({
        from: [publicInternetLabel],
        to: [this.hostname],
        minPort: rng.min,
        maxPort: rng.max,
      });
    });

    return connections;
  }

  /**
   * Converts the Container to the JSON format expected by the Kelda go code.
   * @private
   * @returns {Object} A map that can be converted to JSON and interpreted by the Kelda
   *   Go code.
   */
  toKeldaRepresentation() {
    return {
      id: this.id,
      image: this.image,
      command: this.command,
      env: this.env,
      filepathToContent: this.filepathToContent,
      hostname: this.hostname,
    };
  }
}

class Secret {
  /**
   * Secret represents the name of a secret to extract from the Vault secret
   * store. The value is stored encrypted in a Vault instance running in the
   * cluster. Only the value is considered secret -- names should not contain
   * private information as they are expected to be saved in insecure locations
   * such as user blueprints.
   *
   * A secret association is created by running the Kelda `secret` command. For
   * example, running `kelda secret foo bar` creates a secret named `foo` that
   * can be referenced using this type.
   *
   * @param {string} name - The name of the Secret.
   */
  constructor(name) {
    this.nameOfSecret = name;
  }
}

class RuntimeValue {
  /**
   * RuntimeValue represents metadata about a container that is only known when
   * the container is about to be booted, such as the container's public IP.
   *
   * @param {string} resourceKey - An identifier for what runtime information
   * should be used. The key must match the key defined in the deployment
   * engine.
   */
  constructor(resourceKey) {
    this.resourceKey = resourceKey;
  }
}

/**
 * hostIP is the {@link RuntimeValue} for the public IP of the machine the container is
 * running on.
 *
 * @example <caption>Set the environment variable HOST_IP of myContainer1 and myContainer2
 * to be the public IP of the machine that each container is running on. If myContainer1
 * and myContainer2 are launched on different machines, they will be assigned different
 * HOST_IP values accordingly.</caption>
 * myContainer1.setEnv('HOST_IP', hostIP);
 * myContainer2.setEnv('HOST_IP', hostIP);
 */
const hostIP = new RuntimeValue('host.ip');

/**
 * Attempts to convert `objects` into an array of objects that
 * define allowFrom.
 * If `objects` is an Array, it asserts that each element is connectable. If
 * it's just a single object, boxConnectable asserts that it is connectable,
 * and if so, returns it as a single-element Array.
 * @private
 *
 * @param {Connectable|Connectable[]} objects - The Connectables to be boxed.
 * @returns {Connectable[]} The boxed Connectables.
 */
function boxConnectable(objects) {
  if (isConnectable(objects)) {
    return [objects];
  }

  if (!Array.isArray(objects)) {
    throw new Error('not an array of connectable objects (was ' +
            `${stringify(objects)})`);
  }
  objects.forEach((x, i) => {
    if (!isConnectable(x)) {
      throw new Error(
        `item at index ${i} (${stringify(x)}) cannot be connected to`);
    }
  });
  return objects;
}


/**
 * Interface for classes that can allow inbound traffic.
 *
 *  @interface
 */
// Connectable is never used because it's defining an interface for creating
// JsDoc.
// eslint-disable-next-line no-unused-vars
class Connectable {
  /**
   * Allows traffic from src on port
   *
   * @param {Container} src - The container that can initiate connections.
   * @param {int|Port|PortRange} port - The ports to allow traffic on.
   * @returns {void}
   */
  allowFrom(src, port) { // eslint-disable-line
    throw new Error('not implemented');
  }
}

/**
 * Returns whether x can allow inbound connections.
 * @private
 *
 * @param {Object} x - The object to check
 * @returns {boolean} Whether x can be connected to
 */
function isConnectable(x) {
  return typeof (x.allowFrom) === 'function';
}

/**
 * allow is a utility function to allow calling `allowFrom` on groups of
 * containers.
 *
 * @param {Container|publicInternet} src - The containers that can
 *   initiate a connection.
 * @param {Connectable[]} dst - The objects that traffic can be sent to.
 *   Examples of connectable objects are Containers, LoadBalancers, publicInternet,
 *   and user-defined objects that implement allowFrom.
 * @param {int|Port|PortRange} port - The ports that traffic is allowed on.
 * @returns {void}
 */
function allow(src, dst, port) {
  boxConnectable(dst).forEach((c) => {
    c.allowFrom(src, port);
  });
}

class Connection {
  /**
   * Creates a Connection.
   * @constructor
   *
   * @param {string[]} from - A list of hosts that allow connections.
   * @param {PortRange} ports - The port numbers which are allowed.
   */
  constructor(from, ports) {
    this.minPort = ports.min;
    this.maxPort = ports.max;
    this.from = from;
  }
}

class Range {
  /**
   * Creates a Range object.
   * @constructor
   *
   * @param {integer} min - The minimum of the range (inclusive).
   * @param {integer} max - The maximum of the range (inclusive).
   */
  constructor(min, max) {
    this.min = min;
    this.max = max;
  }
}

/**
 * Checks whether x falls in the Range.
 * @private
 * @param {integer} x - The integer to check.
 * @returns {boolean} True if x is in the Range, and False otherwise.
 */
Range.prototype.inRange = function inRange(x) {
  return (this.min <= x || this.min === undefined) &&
         (this.max === 0 || this.max === undefined || x <= this.max);
};

/**
 * @private
 * @returns {string} The string representation of the Range
 */
Range.prototype.toString = function toString() {
  if (this.max === undefined) {
    return `[${this.min}, inf]`;
  } else if (this.max === this.min) {
    return `${this.max}`;
  }
  return `[${this.min}, ${this.max}]`;
};

const PortRange = Range;

/**
 * Creates a Port object.
 * @constructor
 *
 * @param {integer} p - The port number.
 */
function Port(p) {
  return new PortRange(p, p);
}

/**
 * @returns {Infrastructure} The global infrastructure object.
 */
function getInfrastructure() {
  return _keldaInfrastructure;
}

/**
 * @returns {Object} A map representing the current infrastructure. The map can
 * be converted to JSON and interpreted by the Kelda Go code.
 */
global.getInfrastructureKeldaRepr = function getInfrastructureKeldaRepr() {
  const inf = getInfrastructure();
  return (inf === undefined) ? {} : inf.toKeldaRepresentation();
};

/**
 * Resets global unique counters. Used only for unit testing.
 * @private
 *
 * @returns {void}
 */
function resetGlobals() {
  uniqueIDCounter = 0;
  hostnameCount = {};
  _keldaInfrastructure = undefined;
}

module.exports = {
  Container,
  Infrastructure,
  Image,
  Machine,
  Port,
  PortRange,
  Range,
  Secret,
  LoadBalancer,
  allow,
  getInfrastructure,
  githubKeys,
  hostIP,
  publicInternet,
  resetGlobals,
  baseInfraLocation,
  baseInfrastructure,
};
