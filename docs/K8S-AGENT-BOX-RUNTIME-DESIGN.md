# K8s Agent-Box Runtime — SSH-Reachable Pod Design Note

> Status: **Exploration / not yet approved.** Tracking: #700. This proposes a Kubernetes
> backend for the agent box: a per-tenant pod that an agent reaches over
> SSH, exposing the same `agent-box` stdio MCP surface served today from an
> LXC box. Nothing here is built yet.

## What this closes

Today the in-the-box surface (`cmd/agent-box` — shell + file ops over stdio,
SSH-wrapped) only exists on an **LXC/incus backend**. The transport is SSH:
an agent runs `ssh <user>@<gateway> -- agent-box` and gets a scoped MCP
session pinned into a single box. The gateway is `sshpiper` on the sentinel,
routing by SSH username.

There is no equivalent on Kubernetes. Operators who already run a cluster
have no way to host an agent box as a pod — they'd have to stand up a
separate incus host. This note designs a **K8s backend that preserves the
exact agent-facing contract** (SSH → `ForceCommand agent-box` → stdio MCP)
so an agent cannot tell which runtime it landed on.

This is a *backend behind the existing box vocabulary*, not a parallel
surface. Per the repo's CLI-first convention, the entry point is a
`--runtime=k8s` flag on the existing box-create path; the MCP tool wraps the
same Go function.

## The agent-facing contract (unchanged)

```
ssh <tenant>@<gateway-ip> -- agent-box
        │
        ▼  (sshpiper routes by username)
   ForceCommand /usr/local/bin/agent-box   →   stdio MCP loop
```

Everything below is implementation behind that line. The agent's MCP client,
its known_hosts pin, and its scoped key are identical to the LXC path.

## Decisions (locked for v1)

| Axis | Choice | Rationale |
| --- | --- | --- |
| Pod lifecycle | **Long-lived per tenant** | Mirrors the LXC box model; stable DNS, no cold-start. |
| SSH ingress | **In-cluster `sshpiper` Deployment** | Reuse the sentinel reverse-proxy pattern; per-user fan-out behind one IP. |
| Isolation | **Namespace-per-tenant + default-deny NetworkPolicy** | Soft multi-tenancy; the K8s expression of eBPF deny-by-default + egress allowlist. |

Hard isolation (gVisor/Kata `RuntimeClass`) and ephemeral/pooled lifecycles
are explicitly **out of scope for v1** — see "Deferred".

## Topology

```
                         ns: agent-gateway
                   ┌──────────────────────────────┐
   Agent  ──SSH──▶ │  Service(LB) :22             │
  (MCP cli)        │      │                        │
                   │  sshpiper Deployment          │
                   │  + upstream-controller        │
                   └──────┬───────────────┬────────┘
                          │ route by user │
              ┌───────────▼───┐     ┌─────▼─────────┐
   ns:tenant-a │ StatefulSet │     │ StatefulSet  │ ns:tenant-b
              │ box-0 (sshd + │     │ box-0 (...)  │
              │ agent-box)    │     │              │
              │ NetworkPolicy │     │ NetworkPolicy│
              │ default-deny  │     │ default-deny │
              └───────────────┘     └──────────────┘
```

## Components

### 1. The box (per-tenant StatefulSet)

StatefulSet (not Deployment) for the **stable per-pod DNS name** sshpiper
routes to: `box-0.boxes.<tenant>.svc.cluster.local`.

- One container: `sshd` + the `agent-box` binary on PATH.
- `automountServiceAccountToken: false` — the box is a **leaf**, never a
  kube-apiserver client. Its authz is the scoped JWT seeded inside, exactly
  as on LXC.
- `securityContext`: `runAsNonRoot`, `readOnlyRootFilesystem`,
  `capabilities: drop [ALL]`, seccomp `RuntimeDefault`.
- The tenant's authorized public key is mounted from a per-tenant Secret
  (`AuthorizedKeysFile`), or pulled via `AuthorizedKeysCommand` from the
  control plane — the analog of the sentinel `POST /authorized-keys/sentinel`
  push. **No keys are baked into the image.**

`sshd_config` (image-level):

```
ForceCommand        /usr/local/bin/agent-box   # every session → MCP stdio loop
AuthorizedKeysFile  /etc/agent-box/authorized_keys
PubkeyAuthentication yes
PasswordAuthentication no
PermitTTY           no
AllowTcpForwarding  no                          # no pivoting out of the box
```

`ForceCommand` is the load-bearing line: even a misbehaving client cannot get
a shell — it gets `agent-box` and nothing else.

### 2. Gateway (`sshpiper` Deployment + upstream controller)

`sshpiper` itself is unchanged from the sentinel deployment — it terminates
`:22` (via a `Service type=LoadBalancer`) and routes by SSH username. It stays
a **dumb L4 reverse proxy**; routing state lives one layer up.

The new piece is a thin **upstream controller** replacing the sentinel's
`/authorized-keys`-poll-and-write-YAML loop:

- Watches tenant objects (CRD or labeled namespaces) + their box pods.
- Programs the sshpiper upstream map via the maintained sshpiper **Kubernetes
  plugin** (`PiperUpstream` CRD): `username=<tenant>` →
  `box-0.boxes.<tenant>.svc:22`. CRD-driven removes the file-write race that
  bit the sentinel (the `#301`/`#404` class of bug) entirely.
- Reconciles each tenant's authorized key into the per-tenant Secret.

### 3. Isolation (NetworkPolicy)

Per tenant namespace:

- **Default-deny ingress**, single allow rule: TCP/22 *from the sshpiper pod
  only* (matched by `agent-gateway` namespace + pod label).
- **Default-deny egress**, allowlist: cluster DNS + the control-plane API
  endpoint. This is the K8s expression of the eBPF egress allowlist shipped
  on the LXC backend.

## Mapping to the existing (LXC) architecture

| Containarium (LXC) | This K8s backend |
| --- | --- |
| `agent-box` over stdio, SSH-wrapped | identical — same binary, same `ForceCommand` |
| sshpiper on sentinel :22 | sshpiper Deployment + LB Service :22 |
| sentinel key-sync → YAML | controller → `PiperUpstream` CRD + Secret |
| LXC box per tenant | StatefulSet `box-0` per tenant namespace |
| eBPF deny-by-default + egress allowlist | default-deny NetworkPolicy + egress allowlist |
| sshd 2222 (mgmt) vs sshpiper 22 | mgmt via `kubectl`/RBAC; sshpiper owns 22 |

## CLI-first surface

Per the repo convention, the K8s-ness is a **backend behind the box
vocabulary**, not a new verb tree:

```
containarium box create --runtime=k8s --tenant=<t>
```

templates the namespace + StatefulSet + headless Service + NetworkPolicy +
`PiperUpstream` CRD + per-tenant key Secret. The platform MCP tool wraps the
**same Go function** the CLI handler calls. The backend is selected behind a
runtime interface; LXC stays the default.

## Integrating with an existing cluster (BYO)

The primary target is **not** a dedicated cluster — it is a cluster the
customer already owns, where Containarium's control plane manages boxes
through scoped access it is granted. This is the K8s analog of the
multi-backend-peer / remote-connector pattern: the control plane drives a
cluster it does not own, via a **namespaced operator** running under its own
ServiceAccount — never via the operator's personal credentials.

Design the operator for BYO and the own-cluster case falls out for free: BYO
is the strict superset of constraints.

### What the box demands of the host cluster (it's modest)

The box is a hardened **leaf**, not a cluster client: non-root,
`automountServiceAccountToken: false`, `readOnlyRootFilesystem`, `drop [ALL]`,
seccomp `RuntimeDefault`, no host mounts, no privileged caps. It satisfies the
PodSecurity `restricted` profile as-is, so it passes admission on locked-down
clusters cleanly. That is a feature: the RBAC ask is small and auditable.

### Controller RBAC footprint (namespaced, not cluster-wide)

| Verb scope | Resources | Boundary |
| --- | --- | --- |
| create/get/delete | StatefulSet, Service, Secret, NetworkPolicy | **label-selected tenant namespaces only** |
| CRUD | `PiperUpstream` | gateway namespace only |
| create | Namespace | only if the controller owns tenant-namespace lifecycle |

Shipped as a **Helm chart / operator bundle** the customer reviews and installs.
Containarium then drives the cluster through the controller's ServiceAccount.

### Hard gates — absent these, the design changes (not just config)

| Requirement | Why | Fallback if missing |
| --- | --- | --- |
| **L4/TCP ingress** (LoadBalancer / NodePort / Gateway API `TCPRoute`) | SSH is TCP; K8s Ingress is HTTP-only | NodePort + external LB; `kubectl port-forward` for dev |
| **NetworkPolicy-enforcing CNI** (Calico, Cilium, …) | default-deny isolation is a **no-op** under a CNI that ignores it | degrade to namespace-only isolation — **must flag loudly** |
| **CRD install rights** (cluster-admin, once) for `PiperUpstream` | the sshpiper Kubernetes plugin is CRD-driven | `yaml` plugin — re-inherits the sentinel file-write race |
| **Namespace-create rights** for the controller | namespace-per-tenant | pin to one shared namespace + label/pod-selector separation (weaker) |

### Degraded modes (named, not silent)

- **No LoadBalancer** (bare-metal, no MetalLB) → NodePort + documented external LB.
- **PodSecurity `restricted` enforced** → box already complies; no action.
- **Single shared namespace mandated** → drop namespace-per-tenant; rely on
  NetworkPolicy pod-selectors + per-box ServiceAccount. Weaker blast radius —
  call it out at create time.

### The make-or-break decision

Whether the customer grants a **one-time cluster-admin CRD install**. With it,
the CRD `kubernetes` plugin gives clean, race-free upstream programming. Without
it, the `yaml` plugin works but re-inherits the file-write race (`#301`/`#404`
class). Document **CRD-install as the happy path, `yaml` as the explicit
fallback** — and surface which mode is active in box status.

## Open questions

1. **sshpiper plugin** — CRD `kubernetes` plugin (eliminates the file-write
   race) vs. the `yaml` plugin run today. Leaning CRD.
2. **Host-key trust** — pre-distribute the gateway host key (ConfigMap →
   agent known_hosts) so first-connect is not a TOFU prompt.
3. **Runtime interface shape** — what the box-backend Go interface looks like
   so LXC and K8s implement the same `Create/Delete/Resolve` contract the
   box-create path consumes.

## Deferred (not in v1)

- **Hard isolation** (gVisor/Kata `RuntimeClass`) — for tenants needing a
  VM/syscall boundary closer to the LXC trust boundary.
- **Ephemeral / pooled lifecycles** — spin-up-on-connect or warm-pool leasing.
- **GPU passthrough** in pods (`nvidia.com/gpu` resource + device plugin).
- **Cross-cluster / multi-pool** fan-out (the K8s analog of multi-backend
  peers).
