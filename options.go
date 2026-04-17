package sori

type PackageOptions struct {
	ConfigBlob        []byte
	RequireConfigBlob bool
}

type PushOptions struct {
	Target RemoteTarget
}

type FetchOptions struct {
	Concurrency             int
	RequireEmptyDestination bool
}

type ReferrerOptions struct {
	Target RemoteTarget
}
