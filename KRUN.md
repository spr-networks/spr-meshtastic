# krun branch status

TCP mode uses the krun guest's SPR-managed TAP and is covered by the branch
configuration. Direct USB serial mode is not yet equivalent: passing a host
`/dev/ttyUSB*` node in an OCI spec does not provide a physical USB device to
the libkrun guest kernel.

Serial mode needs an explicit virtio-serial/USB transport or a narrow host
serial-to-vsock bridge. It must not be represented as working until that
transport is added and tested on the router.
