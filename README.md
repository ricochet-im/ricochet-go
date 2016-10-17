Experimental!
-------------

This is an experimental, in-development Ricochet client. If you want something
you can use, try the mainline [Ricochet](https://github.com/ricochet-im/ricochet).

Compared to the existing Ricochet client, the idea here is to:

* Implement a client in Go, because it's memory-safe, easier to write and contribute to, and doesn't depend on a huge UI library
* Split the client into a multiprocess RPC backend/frontend architecture, so that UI implementations can be easily developed in any language/environment
* Build a code base that is easier and quicker to experiment with
* Create some forward momentum

This design has some interesting benefits:

* All network-facing and critical logic is in Go, but frontends can be in any language -- Go currently lacks decent UI frameworks. This could also be useful for mobile applications.
* The existing Qt UI can be adapted as a frontend, without any UX changes
* The backend is headless and could be run remotely (over an authorized hidden service, perhaps). Frontends are detachable and interchangable. It's possible to use multiple frontends simultaneously.
* UI and network components can be sandboxed separately on systems that support it (e.g. Subgraph)

Status
------

This is not ready or safe to use. Some functionality works if you get a proper environment set up. Development notes are available at in the Projects or Issues tab. Pull requests & thoughts always welcome.

Architecture
------------

**core** implements Ricochet's client logic. It currently depends on [bulb](https://github.com/yawning/bulb) and @s-rah's [go-ricochet](https://github.com/s-rah/go-ricochet) protocol implementation.

**rpc** defines a [gRPC](http://www.grpc.io/) and [protobuf](https://developers.google.com/protocol-buffers/) API for communication between the client backend and frontend. This API is for trusted backends to communicate with frontend UI clients, and it's expected that both will usually be on the same machine and invisible to the end-user. Anything capable of speaking gRPC could implement a frontend.

**backend** is the backend application, providing the gRPC server and using the core implementation.

**cli** is the first frontend, a Go readline-style text-only client.
