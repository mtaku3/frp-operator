package digitalocean

// DropletAPIForTest re-exports the internal dropletAPI interface so tests
// can inject stubs via SetDropletAPIFactory.
type DropletAPIForTest = dropletAPI
