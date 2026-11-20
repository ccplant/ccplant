package sessionrunner

// ProvisionerPort is the runtime contract shared by session provisioning and
// status observers. It lives in core so HTTP adapters do not depend on the
// Kubernetes implementation merely to address the provisioner.
const ProvisionerPort = 9001
