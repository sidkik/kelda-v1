package version

// EmptyValue is the value we use when running a version that wasn't compiled
// by `make`. This is helpful for telling when we're running in a unit test.
const EmptyValue = "set-by-make"

// Version is the latest tag on git for releases. On non-release commits, it may
// include additional information such as the most recent commit hash.
var Version = EmptyValue

// KeldaImage is the name of the Kelda Docker image.
var KeldaImage = "ghcr.io/sidkik/kelda:0.15.14" //EmptyValue
