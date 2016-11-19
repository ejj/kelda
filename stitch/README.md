# Quilt.js
Documentation for quilt.js

## Container
The Container object represents a container to be deployed.

### Specifying the Image
The first argument of the `Container` constructor is the image that container
should run.

#### Repository Link
If a `string` is supplied, the image at that repository is used.

#### Dockerfile
Instead of supplying a link to a pre-built image, Quilt also support building
images in the cluster. When specifying a `Dockerfile` to be built, an `Image`
object must be passed to the `Container` constructor.

For example,
```
new Container(new Image("my-image-name",
  "FROM nginx\n" +
  "RUN cd /web_root && git clone github.com/my/web_repo"
))
```
would deploy an image called `my-image-name` built on top of the `nginx` image,
with the `github.com/my/web_repo` repository cloned into `/web_root`.

If the Dockerfile is saved as a file, it can simply be `read` in:
```
new Container(new Image("my-image-name", read("./Dockerfile")))
```

If a user runs a spec that uses a custom image, then runs another spec that
changes the contents of that image's Dockerfile, the image is re-built and
all containers referencing that Dockerfile are restarted with the new image.

If multiple containers specify the same Dockerfile, the same image is reused for
all containers.

If two images with the same name but different Dockerfiles are referenced, an
error is thrown.

### Container.filepathToContent

`Container.filepathToContent` defines text files to be installed on the container
before the container starts. Both the key and value are `string`s.

For example,
```
{
  "/etc/myconf": "foo"
}
```
would create a file at `/etc/myconf` containing the text `foo`.

```
new Container("haproxy").withFiles({
  "/etc/myconf": "foo"
});
```
would create a `haproxy` instance with a text file `/etc/myconf` containing `foo`.

If the files change after the container boots, Quilt does not restart the container.
However, if the file content specified by `filepathToContent` changes, Quilt will
destroy the old container and boot a new one with the proper files.

The files are installed with permissions `0644`. Parent directories are
automatically created.

### Container.hostname()

`Container.hostname` gets the container's hostname. If the container has no
hostname, an error is thrown.

### Container.setHostname()

`Container.setHostname` gives the container a hostname at which the container
can be reached.

If multiple containers have the same hostname, an error is thrown during the
vetting process.

## Machine
The Machine object represents a machine to be deployed.

Its attributes are:
- `role` *string*: The Quilt role the machine will run as. *required*
    - Master
    - Worker
- `provider` *string*: The machine provider. *required*
    - Amazon
    - DigitalOcean
    - Google
- `region` *string*: The region the machine will run in. *optional*
    - Provider-specific
- `size` *string*: The instance type. *optional*
    - Provider-specific
- `cpu` *Range* or *int*: The desired number of CPUs. *optional*
- `ram` *Range* or *int*: The desired amount of RAM in Gib. *optional*
- `diskSize` *int*: The desired amount of disk space in GB. *optional*
- `floatingIp` *string*: A reserved IP to associate with the machine. *optional*
- `sshKeys` *[]string*: Public keys to allow login into the machine. *optional*
- `reserved` *bool*: Whether the machine should run as a reserved instance. *optional*
    - Defaults to `true`
    - Non-reserved instances are only supported on the `Amazon` provider.
