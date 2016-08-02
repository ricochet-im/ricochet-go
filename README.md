This is an idea for the architecture of a Ricochet client. Don't get excited. It doesn't do anything.

The idea is to implement all client backend logic in Go, and export a RPC API for frontends.

Benefits:

* We can have all network-facing and critical logic in Go, without being forced to use Go for frontends (because it lacks decent UI capability)
* We can keep the current Qt UI implementation as one frontend
* It's easy to build new frontends in anything that can use gRPC (like **cli**)
* Backends are headless and frontends are detachable and interchangable
* Can do some fancy sandboxing

Other ideas:

* This is currently using RPC only for the backend<->frontend; would it make sense to RPC any other layers or distinct components? Could have security benefits.
* In particular, we still have one process that has access to private keys, tor config, and untrusted network traffic. That sucks.
* Can do frontend connection to backend over authorized onion for advanced setups

Structure:

* **core** is the client logic implementation
* **rpc** has gRPC/protobuf definitions & generated code
* **backend** is the backend RPC server
* **cli** is an example frontend client
