# Storage

`bolt.Open` stores repository and activity metadata in `<data_dir>/gitsync.db`. bbolt permits one process to own a database file. The open call waits up to five seconds for its exclusive lock, then returns an error naming the file and the one-instance-per-`data_dir` requirement.

The bbolt dependency is pinned to `go.etcd.io/bbolt v1.5.0`, the latest stable release when this phase was implemented. It is maintained by the etcd project and provides the required embedded, transactional key/value store. All record and index changes happen in one bbolt write transaction; reads use read transactions. Schema version 1 is stored in `meta/schema_version`.

`store.NewMemory` returns repository and activity interfaces backed by the same synchronized in-memory data. It is intended for tests in later phases. `store/storetest.RunSuite` is the shared behavioral contract used by the memory and bbolt implementations.
