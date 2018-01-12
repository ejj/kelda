/* eslint-env mocha */

/* eslint-disable import/no-extraneous-dependencies, no-underscore-dangle, require-jsdoc */
const chai = require('chai');
const chaiSubset = require('chai-subset');
const rewire = require('rewire');
const sinon = require('sinon');

const b = rewire('./bindings.js');

chai.use(chaiSubset);
const { expect } = chai;

describe('Bindings', () => {
  let infra;

  const createBasicInfra = function createBasicInfra() {
    const machine = new b.Machine({ provider: 'Amazon' });
    infra = new b.Infrastructure(machine, machine);
  };

  beforeEach(b.resetGlobals);

  const checkMachines = function checkMachines(expectedSubset) {
    const { machines } = infra.toKeldaRepresentation();
    expect(machines).to.containSubset(expectedSubset);
  };

  const checkContainers = function checkContainers(expected) {
    const { containers } = infra.toKeldaRepresentation();
    expect(containers).to.have.lengthOf(expected.length)
      .and.containSubset(expected);
  };

  const checkPlacements = function checkPlacements(expected) {
    const { placements } = infra.toKeldaRepresentation();
    expect(placements).to.have.lengthOf(expected.length)
      .and.containSubset(expected);
  };

  const checkLoadBalancers = function checkLoadBalancers(expected) {
    const { loadBalancers } = infra.toKeldaRepresentation();
    expect(loadBalancers).to.have.lengthOf(expected.length)
      .and.containSubset(expected);
  };

  const checkConnections = function checkConnections(expected) {
    const { connections } = infra.toKeldaRepresentation();
    expect(connections).to.have.lengthOf(expected.length)
      .and.containSubset(expected);
  };

  const checkInRange = function checkInRange(min, max, value, expected) {
    const range = new b.Range(min, max);
    expect(range.inRange(value)).to.equal(expected);
  };

  describe('Range', () => {
    it('both ends are specified', () => {
      checkInRange(2, 4, 3, true);
      checkInRange(2, 3, 4, false);
    });
    it('no max', () => {
      checkInRange(2, 0, 3, true);
      checkInRange(2, 0, 1, false);
    });
    it('range is one number', () => {
      checkInRange(2, 2, 2, true);
      checkInRange(2, 2, 3, false);
    });
    it('value is on boundary', () => {
      checkInRange(2, 4, 4, true);
      checkInRange(2, 4, 2, true);
    });
    it('undefined max results in a lower-bounded range', () => {
      const range = new b.Range(2);
      expect(range.inRange(6)).to.equal(true);
      expect(range.inRange(2)).to.equal(true);
      expect(range.inRange(1)).to.equal(false);
    });
    it('undefined min and max accepts any value', () => {
      const range = new b.Range();
      expect(range.inRange(6)).to.equal(true);
      expect(range.inRange(2)).to.equal(true);
      expect(range.inRange(1)).to.equal(true);
    });
  });

  describe('Machine', () => {
    it('basic', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        region: 'us-west-2',
        size: 'm4.large',
        cpu: new b.Range(2, 4),
        ram: new b.Range(4, 8),
        diskSize: 32,
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      // Since a separate test suite checks that the Infrastructure constructor
      // creates the right number and kinds of machines, this and many other
      // tests only check a subset of the created machines in order to test the
      // desired parts of the API.
      checkMachines([{
        provider: 'Amazon',
        region: 'us-west-2',
        size: 'm4.large',
        diskSize: 32,
        sshKeys: ['key1', 'key2'],
      }]);
    });
    it('throws error when no Provider specified', () => {
      expect(() => new b.Machine({})).to.throw('Machine must specify a provider ' +
        '(accepted values are Amazon, DigitalOcean, Google, and Vagrant');
    });
    it('chooses size when provided ram and cpu', () => {
      const machine = new b.Machine({
        provider: 'Google',
        cpu: new b.Range(2, 4),
        ram: new b.Range(6, 8),
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Google',
        size: 'n1-standard-2',
      }]);
    });
    it('chooses size when provided only minimum ram and cpu', () => {
      const machine = new b.Machine({
        provider: 'Google',
        cpu: new b.Range(2),
        ram: new b.Range(6),
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Google',
        size: 'n1-standard-2',
      }]);
    });
    it('chooses size when ram and cpu do not have max', () => {
      const machine = new b.Machine({
        role: 'Worker',
        provider: 'Google',
        cpu: new b.Range(1, 0),
        ram: new b.Range(1, 0),
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Google',
        size: 'g1-small',
      }]);
    });
    it('chooses size when ram and cpu are exact matches', () => {
      const machine = new b.Machine({
        role: 'Worker',
        provider: 'Amazon',
        cpu: new b.Range(2, 2),
        ram: new b.Range(8, 8),
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        size: 'm4.large',
      }]);
    });
    it('chooses size when only cpu is specifed', () => {
      const machine = new b.Machine({
        role: 'Worker',
        provider: 'Amazon',
        cpu: new b.Range(2, 2),
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        size: 'c4.large',
      }]);
    });
    it('chooses size when only ram is specifed', () => {
      const machine = new b.Machine({
        role: 'Worker',
        provider: 'Amazon',
        ram: new b.Range(8, 8),
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        size: 'm4.large',
      }]);
    });
    it('chooses size when neither ram nor cpu are provided', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        // This should choose m3.medium and not t2.micro (because
        // IgnoredByKelda is set to true for t2.micro).
        size: 'm3.medium',
      }]);
    });
    it('t2.micro can be used if explicitly specified by user', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        size: 't2.micro',
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        size: 't2.micro',
      }]);
    });
    it('errors if provided size does not meet RAM or ' +
       'CPU requirements for DigitalOcean instance', () => {
      expect(() => new b.Machine({
        role: 'Worker',
        provider: 'DigitalOcean',
        size: 'm-16gb',
        sshKeys: ['key1', 'key2'],
        cpu: new b.Range(3, 4),
        ram: new b.Range(6, 8),
        preemptible: false,
      })).to.throw('Requested size \'m-16gb\' does not meet ' +
                   'RAM \'[6, 8]\' or CPU \'[3, 4]\' requirements. ' +
                   'Instance RAM: \'16\', Instance CPU: \'2\'');
    });
    it('errors if provided size does not meet RAM or ' +
       'CPU requirements for Amazon instance', () => {
      expect(() => new b.Machine({
        role: 'Worker',
        provider: 'Amazon',
        size: 'm4.large',
        sshKeys: ['key1', 'key2'],
        cpu: 4,
        ram: 6,
        preemptible: false,
      })).to.throw('Requested size \'m4.large\' does not meet ' +
                   'RAM \'6\' or CPU \'4\' requirements. ' +
                   'Instance RAM: \'8\', Instance CPU: \'2\'');
    });
    it('errors if provided size is not valid', () => {
      expect(() => new b.Machine({
        role: 'Worker',
        provider: 'Google',
        size: 'badName',
        sshKeys: ['key1', 'key2'],
        cpu: 2,
        ram: 6,
        preemptible: false,
      })).to.throw('Invalid machine size "badName" for provider Google');
    });
    it('chooses default region when region is not provided ' +
       'for Google', () => {
      const machine = new b.Machine({
        provider: 'Google',
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Google',
        region: 'us-east1-b',
      }]);
    });
    it('chooses default region when region is not provided ' +
       'for Amazon', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        region: 'us-west-1',
      }]);
    });
    it('chooses default region when region is not provided ' +
       'for DigitalOcean', () => {
      const machine = new b.Machine({
        provider: 'DigitalOcean',
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'DigitalOcean',
        region: 'sfo1',
      }]);
    });
    it('uses empty string as region for Vagrant', () => {
      const machine = new b.Machine({
        provider: 'Vagrant',
        sshKeys: ['key1', 'key2'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Vagrant',
        region: '',
      }]);
    });
    it('uses provided region when region is provided', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        sshKeys: ['key1', 'key2'],
        region: 'eu-west-1',
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        region: 'eu-west-1',
      }]);
    });
    it('errors when passed invalid optional arguments', () => {
      expect(() => new b.Machine({ provider: 'Amazon', badArg: 'foo' })).to
        .throw('Unrecognized keys passed to Machine constructor: badArg');
      expect(() => new b.Machine({
        badArg: 'foo', provider: 'Amazon', alsoBad: 'bar' }))
        .to.throw('Unrecognized keys passed to Machine constructor: ');
    });
    it('cpu and ram are set based on specified size', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        size: 'm4.large',
      });
      // This test can't use checkMachines, because that function uses the Kelda
      // representation, and the CPU and RAM are removed from the final
      // representation that is sent to Kelda.
      expect(machine.cpu).to.equal(2);
      expect(machine.ram).to.equal(8);
    });
    it('cpu and ram are set to exact values when passed in as ranges', () => {
      // This test verifies that the Machine constructor sets the cpu and ram properties
      // to be the exact values of the CPU and RAM of of the machine that will be
      // launched as a result of the passed-in resource constraints.
      const machine = new b.Machine({
        provider: 'Amazon',
        cpu: new b.Range(4, 6),
        ram: new b.Range(8, 16),
      });
      // Like the above test, this test can't use checkMachines.
      expect(machine.size).to.equal('m4.xlarge');
      expect(machine.cpu).to.equal(4);
      expect(machine.ram).to.equal(16);
    });
    it('cpu and ram are not included in JSON representation', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        size: 'm4.large',
      });
      infra = new b.Infrastructure(machine, machine);
      expect(machine).to.have.property('cpu');
      expect(machine).to.have.property('ram');
      const { machines } = infra.toKeldaRepresentation();
      machines.forEach((m) => {
        expect(m).to.not.have.property('cpu');
        expect(m).to.not.have.property('ram');
      });
    });
    it('hash independent of SSH keys', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        region: 'us-west-2',
        size: 'm4.large',
        cpu: new b.Range(2, 4),
        ram: new b.Range(4, 8),
        diskSize: 32,
        sshKeys: ['key3'],
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        role: 'Worker',
        provider: 'Amazon',
        region: 'us-west-2',
        size: 'm4.large',
        diskSize: 32,
        sshKeys: ['key3'],
      }]);
    });
    it('replicate', () => {
      const baseMachine = new b.Machine({ provider: 'Amazon' });
      infra = new b.Infrastructure(baseMachine.replicate(2), baseMachine);
      checkMachines([
        {
          role: 'Master',
          provider: 'Amazon',
        },
        {
          role: 'Master',
          provider: 'Amazon',
        },
      ]);
    });
    it('replicate independent', () => {
      const baseMachine = new b.Machine({ provider: 'Amazon' });
      const machines = baseMachine.replicate(2);
      machines[0].sshKeys.push('key');
      infra = new b.Infrastructure(machines[0], machines[1]);
      checkMachines([
        {
          role: 'Master',
          provider: 'Amazon',
          sshKeys: ['key'],
        },
        {
          role: 'Worker',
          provider: 'Amazon',
        },
      ]);
    });
    it('set floating IP', () => {
      const master = new b.Machine({ provider: 'Amazon' });
      const machineWithFloatingIP = new b.Machine({
        provider: 'Amazon', floatingIp: 'xxx.xxx.xxx.xxx' });

      infra = new b.Infrastructure(master, machineWithFloatingIP);
      checkMachines([{
        role: 'Worker',
        provider: 'Amazon',
        floatingIp: 'xxx.xxx.xxx.xxx',
        sshKeys: [],
      }]);
    });
    it('preemptible attribute', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        preemptible: true,
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        provider: 'Amazon',
        preemptible: true,
      }]);
    });
  });

  describe('Container', () => {
    beforeEach(createBasicInfra);
    it('basic', () => {
      const container = new b.Container('host', 'image');
      container.deploy(infra);
      checkContainers([{
        id: '293fc7ad8a799d3cf2619a3db7124b0459f395cb',
        image: new b.Image('image'),
        hostname: 'host',
        command: [],
        env: {},
        filepathToContent: {},
      }]);
    });
    it('errors when passed invalid optional arguments', () => {
      expect(() => new b.Container('host', 'image', { badArg: 'foo' })).to
        .throw('Unrecognized keys passed to Container constructor: badArg');
      expect(() => new b.Container('host', 'image',
        { badArg: 'foo', command: [], alsoBad: 'bar' }))
        .to.throw('Unrecognized keys passed to Container constructor: ');
    });
    it('containers are not duplicated', () => {
      const container = new b.Container('host', 'image');
      container.deploy(infra);
      container.deploy(infra);
      checkContainers([{
        id: '293fc7ad8a799d3cf2619a3db7124b0459f395cb',
        image: new b.Image('image'),
        hostname: 'host',
        command: [],
        env: {},
        filepathToContent: {},
      }]);
    });
    it('command', () => {
      const container = new b.Container('host', 'image', {
        command: ['arg1', 'arg2'],
      });
      container.deploy(infra);
      checkContainers([{
        id: '9d0d496d49bed06e7c76c2b536d7520ccc1717f2',
        image: new b.Image('image'),
        command: ['arg1', 'arg2'],
        hostname: 'host',
        env: {},
        filepathToContent: {},
      }]);
    });
    it('env', () => {
      const c = new b.Container('host', 'image');
      c.env.foo = 'bar';
      c.env.secretEnv = new b.Secret('secret');
      c.env.ipEnv = b.hostIP;
      c.deploy(infra);
      checkContainers([{
        id: 'd6eb13faa23199615c6fbecff35ada1626f8ccb6',
        image: new b.Image('image'),
        command: [],
        env: {
          foo: 'bar',
          secretEnv: { nameOfSecret: 'secret' },
          ipEnv: { resourceKey: 'host.ip' },
        },
        hostname: 'host',
        filepathToContent: {},
      }]);
    });
    it('hostname', () => {
      const c = new b.Container('host', new b.Image('image'));
      c.deploy(infra);
      expect(c.getHostname()).to.equal('host.q');
      checkContainers([{
        id: '293fc7ad8a799d3cf2619a3db7124b0459f395cb',
        image: new b.Image('image'),
        command: [],
        env: {},
        filepathToContent: {},
        hostname: 'host',
      }]);
    });
    it('repeated hostnames don\'t conflict', () => {
      const x = new b.Container('host', 'image');
      const y = new b.Container('host', 'image');
      x.deploy(infra);
      y.deploy(infra);
      checkContainers([{
        id: '293fc7ad8a799d3cf2619a3db7124b0459f395cb',
        image: new b.Image('image'),
        command: [],
        env: {},
        filepathToContent: {},
        hostname: 'host',
      }, {
        id: '968bcf8c6d235afbc88aec8e1bdddc506714a0b8',
        image: new b.Image('image'),
        command: [],
        env: {},
        filepathToContent: {},
        hostname: 'host2',
      }]);
    });
    it('Container.hostname and LoadBalancer.hostname don\'t conflict', () => {
      const container = new b.Container('foo', 'image');
      const serv = new b.LoadBalancer('foo', []);
      expect(container.getHostname()).to.equal('foo.q');
      expect(serv.hostname()).to.equal('foo2.q');
    });
    it('hostnames returned by uniqueHostname cannot be reused', () => {
      const containerA = new b.Container('host', 'ignoreme');
      const containerB = new b.Container('host', 'ignoreme');
      const containerC = new b.Container('host2', 'ignoreme');
      const hostnames = [containerA, containerB, containerC]
        .map(c => c.getHostname());
      const hostnameSet = new Set(hostnames);
      expect(hostnames.length).to.equal(hostnameSet.size);
    });
    it('clone increments existing index if one exists', () => {
      const containerA = new b.Container('host', 'ignoreme');
      const containerB = containerA.clone();
      const containerC = containerB.clone();
      expect(containerA.getHostname()).to.equal('host.q');
      expect(containerB.getHostname()).to.equal('host2.q');
      expect(containerC.getHostname()).to.equal('host3.q');
    });
    it('duplicate hostname causes error', () => {
      const x = new b.Container('host', 'image');
      x.hostname = 'host';
      x.deploy(infra);
      const y = new b.Container('host', 'image');
      y.hostname = 'host';
      y.deploy(infra);
      expect(() => infra.toKeldaRepresentation()).to
        .throw('hostname "host" used multiple times');
    });
    it('image dockerfile', () => {
      const z = new b.Container('host', new b.Image('name', 'dockerfile'));
      z.deploy(infra);
      checkContainers([{
        id: 'fbc9aedb5af0039b8cf09bca2ef5771467b44085',
        image: new b.Image('name', 'dockerfile'),
        hostname: 'host',
        command: [],
        env: {},
        filepathToContent: {},
      }]);
    });
  });

  describe('Container attributes', () => {
    const hostname = 'host';
    const image = new b.Image('image');
    const command = ['arg1', 'arg2'];

    const env = { foo: 'bar' };

    const filepathToContent = {
      qux: new b.Secret('quuz'),
      pubIP: b.hostIP,
    };
    const jsonFilepathToContent = {
      qux: { nameOfSecret: 'quuz' },
      pubIP: { resourceKey: 'host.ip' },
    };
    beforeEach(createBasicInfra);
    it('withEnv', () => {
      // The blueprint ID is different than the Container created with the
      // constructor because the hostname ID increases with each withEnv
      // call.
      const id = '6d9ccb3b68beb44cc372936f66fafbf6deac4a27';
      const container = new b.Container(hostname, image, {
        command,
        filepathToContent,
      }).withEnv(env);
      container.deploy(infra);
      checkContainers([{
        id,
        image,
        command,
        env,
        filepathToContent: jsonFilepathToContent,
        hostname: 'host2',
      }]);
    });
    it('constructor', () => {
      const id = '254ee1916fecf9811f1b4d02393a1f561ffd86f8';
      const container = new b.Container(hostname, image, {
        command, env, filepathToContent,
      });
      container.deploy(infra);
      checkContainers([{
        id,
        hostname,
        image,
        command,
        env,
        filepathToContent: jsonFilepathToContent,
      }]);
    });
  });

  describe('Placement', () => {
    let target;
    beforeEach(() => {
      createBasicInfra();
      target = new b.Container('host', 'image');
      target.deploy(infra);
    });
    it('MachineRule size, region, provider', () => {
      target.placeOn({
        size: 'm4.large',
        region: 'us-west-2',
        provider: 'Amazon',
      });
      checkPlacements([{
        targetContainer: 'host',
        exclusive: false,
        region: 'us-west-2',
        provider: 'Amazon',
        size: 'm4.large',
      }]);
    });
    it('MachineRule size, provider', () => {
      target.placeOn({
        size: 'm4.large',
        provider: 'Amazon',
      });
      checkPlacements([{
        targetContainer: 'host',
        exclusive: false,
        provider: 'Amazon',
        size: 'm4.large',
      }]);
    });
    it('MachineRule floatingIp', () => {
      target.placeOn({
        floatingIp: 'xxx.xxx.xxx.xxx',
      });
      checkPlacements([{
        targetContainer: 'host',
        exclusive: false,
        floatingIp: 'xxx.xxx.xxx.xxx',
      }]);
    });
  });
  describe('LoadBalancer', () => {
    beforeEach(createBasicInfra);
    it('basic', () => {
      const lb = new b.LoadBalancer('web_tier', [new b.Container('host', 'nginx')]);
      lb.deploy(infra);
      checkLoadBalancers([{
        name: 'web_tier',
        hostnames: ['host'],
      }]);
    });
    it('multiple containers', () => {
      const lb = new b.LoadBalancer('web_tier', [
        new b.Container('host', 'nginx'),
        new b.Container('host', 'nginx'),
      ]);
      lb.deploy(infra);
      checkLoadBalancers([{
        name: 'web_tier',
        hostnames: [
          'host',
          'host2',
        ],
      }]);
    });
    it('duplicate load balancers', () => {
      /* Conflicting load balancer names.  We need to generate a couple of dummy
               containers so that the two deployed containers have _refID's
               that are sorted differently lexicographically and numerically. */
      for (let i = 0; i < 2; i += 1) {
        new b.Container('host', 'image'); // eslint-disable-line no-new
      }
      const lb = new b.LoadBalancer('foo', [new b.Container('host', 'image')]);
      lb.deploy(infra);
      for (let i = 0; i < 7; i += 1) {
        new b.Container('host', 'image'); // eslint-disable-line no-new
      }
      const lb2 = new b.LoadBalancer('foo', [new b.Container('host', 'image')]);
      lb2.deploy(infra);
      checkLoadBalancers([
        {
          name: 'foo',
          hostnames: ['host3'],
        },
        {
          name: 'foo2',
          hostnames: ['host11'],
        },
      ]);
    });
    it('get LoadBalancer hostname', () => {
      const foo = new b.LoadBalancer('foo', []);
      expect(foo.hostname()).to.equal('foo.q');
    });
  });
  describe('AllowFrom', () => {
    let foo;
    let bar;
    let fooLoadBalancer;
    beforeEach(() => {
      createBasicInfra();
      foo = new b.Container('foo', 'image');
      foo.deploy(infra);
      fooLoadBalancer = new b.LoadBalancer('fooLoadBalancer', [foo]);
      bar = new b.Container('bar', 'image');
      bar.deploy(infra);
      fooLoadBalancer.deploy(infra);
    });
    it('autobox port ranges', () => {
      bar.allowFrom(foo, 80);
      checkConnections([{
        from: ['foo'],
        to: ['bar'],
        minPort: 80,
        maxPort: 80,
      }]);
    });
    it('port', () => {
      bar.allowFrom(foo, new b.Port(80));
      checkConnections([{
        from: ['foo'],
        to: ['bar'],
        minPort: 80,
        maxPort: 80,
      }]);
    });
    it('port range', () => {
      bar.allowFrom(foo, new b.PortRange(80, 85));
      checkConnections([{
        from: ['foo'],
        to: ['bar'],
        minPort: 80,
        maxPort: 85,
      }]);
    });
    it('connect to invalid port range', () => {
      expect(() => foo.allowFrom(bar, true)).to
        .throw('Input argument must be a number or a Range');
    });
    it('allow connections to publicInternet', () => {
      b.publicInternet.allowFrom(foo, 80);
      checkConnections([{
        from: ['foo'],
        to: ['public'],
        minPort: 80,
        maxPort: 80,
      }]);
    });
    it('allow connections from publicInternet', () => {
      foo.allowFrom(b.publicInternet, 80);
      checkConnections([{
        from: ['public'],
        to: ['foo'],
        minPort: 80,
        maxPort: 80,
      }]);
    });
    it('connect to LoadBalancer', () => {
      fooLoadBalancer.allowFrom(bar, 80);
      checkConnections([{
        from: ['bar'],
        to: ['fooLoadBalancer'],
        minPort: 80,
        maxPort: 80,
      }]);
    });
    it('connect to publicInternet port range', () => {
      expect(() =>
        b.publicInternet.allowFrom(foo, new b.PortRange(80, 81))).to
        .throw('public internet can only connect to single ports ' +
                        'and not to port ranges');
    });
    it('connect from publicInternet port range', () => {
      expect(() =>
        foo.allowFrom(b.publicInternet, new b.PortRange(80, 81))).to
        .throw('public internet can only connect to single ports ' +
                        'and not to port ranges');
    });
    it('allowFrom non-container', () => {
      expect(() => foo.allowFrom(10, 10)).to
        .throw('Containers can only connect to other containers. ' +
                    'Check that you\'re allowing connections from a ' +
                    'container or list of containers, and not from a LoadBalancer ' +
                    'or other object.');
    });
  });
  describe('allow', () => {
    let foo;
    let bar;
    let qux;
    let quuz;
    let fooBarGroup;
    let quxQuuzGroup;
    let lb;
    beforeEach(() => {
      createBasicInfra();

      foo = new b.Container('foo', 'image');
      bar = new b.Container('bar', 'image');
      qux = new b.Container('qux', 'image');
      quuz = new b.Container('quuz', 'image');

      fooBarGroup = [foo, bar];
      quxQuuzGroup = [qux, quuz];
      lb = new b.LoadBalancer('serv', [foo, bar, qux, quuz]);

      foo.deploy(infra);
      bar.deploy(infra);
      qux.deploy(infra);
      quuz.deploy(infra);
      lb.deploy(infra);
    });

    it('both src and dst are lists', () => {
      b.allow(quxQuuzGroup, fooBarGroup, 80);
      checkConnections([
        { from: ['qux', 'quuz'], to: ['foo'], minPort: 80, maxPort: 80 },
        { from: ['qux', 'quuz'], to: ['bar'], minPort: 80, maxPort: 80 },
      ]);
    });

    it('dst is a list', () => {
      b.allow(qux, fooBarGroup, 80);
      checkConnections([
        { from: ['qux'], to: ['foo'], minPort: 80, maxPort: 80 },
        { from: ['qux'], to: ['bar'], minPort: 80, maxPort: 80 },
      ]);
    });

    it('src is a list', () => {
      b.allow(fooBarGroup, qux, 80);
      checkConnections([
        { from: ['foo', 'bar'], to: ['qux'], minPort: 80, maxPort: 80 },
      ]);
    });

    it('src is public', () => {
      b.allow(b.publicInternet, fooBarGroup, 80);
      checkConnections([
        { from: ['public'], to: ['foo'], minPort: 80, maxPort: 80 },
        { from: ['public'], to: ['bar'], minPort: 80, maxPort: 80 },
      ]);
    });

    it('dst is public', () => {
      b.allow(fooBarGroup, b.publicInternet, 80);
      checkConnections([
        { from: ['foo'], to: ['public'], minPort: 80, maxPort: 80 },
        { from: ['bar'], to: ['public'], minPort: 80, maxPort: 80 },
      ]);
    });

    it('dst is a LoadBalancer', () => {
      b.allow(fooBarGroup, lb, 80);
      checkConnections([
        { from: ['foo', 'bar'], to: ['serv'], minPort: 80, maxPort: 80 },
      ]);
    });
  });
  describe('Vet', () => {
    const deploy = () => infra.toKeldaRepresentation();
    it('should error when given namespace contains upper case letters', () => {
      const machine = new b.Machine({ provider: 'Amazon' });
      infra = new b.Infrastructure(
        machine, machine, { namespace: 'BadNamespace' });
      expect(deploy).to.throw('namespace "BadNamespace" contains ' +
                  'uppercase letters. Namespaces must be lowercase.');
    });
    it('connect from undeployed container', () => {
      createBasicInfra();
      const foo = new b.LoadBalancer('foo', []);
      foo.deploy(infra);

      foo.allowFrom(new b.Container('baz', 'image'), 80);
      expect(deploy).to.throw(
        'connection {"from":["baz"],"maxPort":80,"minPort":80,' +
                '"to":["foo"]} references an undefined hostname: baz');
    });
    it('duplicate image', () => {
      createBasicInfra();
      (new b.Container('host', new b.Image('img', 'dk'))).deploy(infra);
      (new b.Container('host', new b.Image('img', 'dk'))).deploy(infra);
      expect(deploy).to.not.throw();
    });
    it('duplicate image with different Dockerfiles', () => {
      createBasicInfra();
      (new b.Container('host', new b.Image('img', 'dk'))).deploy(infra);
      (new b.Container('host', new b.Image('img', 'dk2'))).deploy(infra);
      expect(deploy).to.throw('img has differing Dockerfiles');
    });
    it('machines with same regions/providers', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        region: 'us-west-2',
      });
      infra = new b.Infrastructure(machine, machine);
      expect(deploy).to.not.throw();
    });
    it('machines with different regions', () => {
      const westMachine = new b.Machine({
        provider: 'Amazon', region: 'us-west-2' });
      const eastMachine = new b.Machine({
        provider: 'Amazon', region: 'us-east-2' });

      infra = new b.Infrastructure(westMachine, eastMachine);
      expect(deploy).to.throw('All machines must have the same provider and region. '
        + 'Found providers \'Amazon\' in region \'us-west-2\' and \'Amazon\' in '
        + 'region \'us-east-2\'.');
    });
    it('machines with different providers', () => {
      const amazonMachine = new b.Machine({ provider: 'Amazon', region: '' });
      const doMachine = new b.Machine({ provider: 'DigitalOcean', region: '' });
      infra = new b.Infrastructure(amazonMachine, doMachine);
      expect(deploy).to.throw('All machines must have the same provider and region. '
        + 'Found providers \'Amazon\' in region \'us-west-1\' and \'DigitalOcean\' in '
        + 'region \'sfo1\'.');
    });
  });
  describe('Infrastructure', () => {
    it('using Infrastructure constructor overwrites the default Infrastructure', () => {
      const namespace = 'testing-namespace';
      const machine = new b.Machine({
        provider: 'Amazon',
        region: 'us-west-2',
      });
      infra = new b.Infrastructure([machine], [machine], { namespace });
      expect(infra.toKeldaRepresentation().namespace).to.equal(namespace);
    });
    it('constructor errors when it would overwrite an existing Infrastructure', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        region: 'us-west-2',
      });

      infra = new b.Infrastructure([machine], [machine], { namespace: 'n1' });
      expect(() => new b.Infrastructure([machine], [machine], { namespace: 'n2' }))
        .to.throw('the Infrastructure constructor has already been called once ' +
          '(each Kelda blueprint can only define one Infrastructure)');
    });
    it('master and worker machines added correctly', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        region: 'us-west-2',
      });
      infra = new b.Infrastructure([machine], [machine, machine]);
      checkMachines([{
        role: 'Master',
        provider: 'Amazon',
        region: 'us-west-2',
      }, {
        // The ID is included here because otherwise the containSubset function
        // used in checkMachines will return true, even if there is only one
        // worker and two masters in the actual output.
        role: 'Worker',
        provider: 'Amazon',
        region: 'us-west-2',
      }, {
        role: 'Worker',
        provider: 'Amazon',
        region: 'us-west-2',
      }]);
    });
    it('accepts non-array master and worker as arguments', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
        region: 'us-west-2',
      });
      infra = new b.Infrastructure(machine, machine);
      checkMachines([{
        role: 'Master',
        provider: 'Amazon',
        region: 'us-west-2',
      }, {
        role: 'Worker',
        provider: 'Amazon',
        region: 'us-west-2',
      }]);
    });
    it('errors when no masters are given', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
      });
      expect(() => new b.Infrastructure([], [machine]))
        .to.throw('masters must include 1 or more');
    });
    it('errors when no workers are given', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
      });
      expect(() => new b.Infrastructure([machine], []))
        .to.throw('workers must include 1 or more');
    });
    it('errors when a non-Machine is given as the master', () => {
      const machine = new b.Machine({
        provider: 'Amazon',
      });
      expect(() => new b.Infrastructure(['not a Machine'], [machine]))
        .to.throw('not an array of Machines; item at index 0 ("not a Machine") is not a Machine');
      expect(() => new b.Infrastructure(3, [machine]))
        .to.throw('not an array of Machines (was 3)');
    });
    it('should error when given invalid arguments', () => {
      const machine = new b.Machine({ provider: 'Amazon' });
      expect(() => new b.Infrastructure(machine, machine, { badArg: 'foo' }))
        .to.throw('Unrecognized keys passed to Infrastructure constructor: badArg');
    });
    it('should not throw when passed a empty optional argument', () => {
      const machine = new b.Machine({ provider: 'Amazon' });
      expect(() => new b.Infrastructure(machine, machine, {})).to.not.throw();
    });
  });
  describe('Query', () => {
    const machine = new b.Machine({ provider: 'Amazon' });
    it('namespace', () => {
      infra = new b.Infrastructure(
        machine, machine, { namespace: 'mynamespace' });
      expect(infra.toKeldaRepresentation().namespace).to.equal(
        'mynamespace');
    });
    it('default namespace', () => {
      createBasicInfra();
      expect(infra.toKeldaRepresentation().namespace).to.equal(
        'kelda');
    });
    it('admin ACL', () => {
      infra = new b.Infrastructure(
        machine, machine, { adminACL: ['local'] });
      expect(infra.toKeldaRepresentation().adminACL).to.eql(
        ['local']);
    });
    it('default admin ACL', () => {
      createBasicInfra();
      expect(infra.toKeldaRepresentation().adminACL).to.eql([]);
    });
  });
  describe('githubKeys()', () => {});
  describe('baseInfrastructure()', () => {
    let fsExistsStub;
    let revertFs;

    beforeEach(() => {
      fsExistsStub = sinon.stub();
      const fsMock = {
        existsSync: fsExistsStub,
      };
      revertFs = b.__set__('fs', fsMock);
    });

    afterEach(() => {
      revertFs();
      fsExistsStub.resetBehavior();
    });

    it('should error when the infrastructure doesn\'t exist', () => {
      fsExistsStub.withArgs(b.baseInfraLocation).returns(false);
      expect(() => b.baseInfrastructure()).to.throw(
        'no base infrastructure. Use `kelda init` to create one.');
    });

    it('should return the infrastructure object when the infra exists', () => {
      const expected = 'someInfrastructure';

      const getInfraStub = sinon.stub();
      getInfraStub.returns(expected);
      const revertGetInfra = b.__set__('getBaseInfrastructure', getInfraStub);

      fsExistsStub.withArgs(b.baseInfraLocation).returns(true);

      let output;
      const expectedPass = () => {
        output = b.baseInfrastructure();
      };

      expect(expectedPass).to.not.throw();
      expect(output).to.equal(expected);

      revertGetInfra();
    });
  });
  describe('getInfrastructureKeldaRepr()', () => {
    it('should return an empty object when no Infrastructure was created', () => {
      b.__set__('_keldaInfrastructure', undefined);
      expect(global.getInfrastructureKeldaRepr()).to.deep.equal({});
    });
    it('should return the correct infra object when an Infrastructure exists', () => {
      const machine = new b.Machine({ provider: 'Amazon', size: 'm3.medium' });
      infra = new b.Infrastructure(machine, machine);
      const expected = {
        adminACL: [],
        connections: [],
        containers: [],
        loadBalancers: [],
        machines: [
          {
            preemptible: false,
            provider: 'Amazon',
            region: 'us-west-1',
            role: 'Master',
            size: 'm3.medium',
            sshKeys: [],
          },
          {
            preemptible: false,
            provider: 'Amazon',
            region: 'us-west-1',
            role: 'Worker',
            size: 'm3.medium',
            sshKeys: [],
          },
        ],
        namespace: 'kelda',
        placements: [],
      };
      expect(global.getInfrastructureKeldaRepr()).to.containSubset(expected);
    });
  });
});
