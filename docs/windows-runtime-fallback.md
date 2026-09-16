# Windows runtime environment fallback

Windows fallback introduced in `process-ancestry-v3`; its rules are preserved in `process-ancestry-v4`.

The existing process ancestry and identity checks remain primary. Only an
`unknown / none` result uses the Windows-only fallback, inside the existing
five-second disposable helper. Signature mismatches and successful matches are
preserved. Helper failure/timeout still returns unknown; no unbounded fallback
runs in the business process. macOS and Linux behavior is unchanged.

- WorkBuddy: resolve the inherited `BASH_ENV` path to
  `vendor/shim/shell-runtime-bash-env.sh`; require a regular script file and
  sibling runtime `product.json` with `productName=WorkBuddy` and
  `authentication.id=workbuddy-desktop`. Do not assume the install drive,
  username, or installation directory name. Native Windows and Git Bash
  `/c/...` paths are supported. JSON reads are capped at 1 MiB.
- Doubao/DoubaoWork: require both inherited `CUA_BASE_PACK_DIR` and
  `CUA_DLC_DIR` to reference existing directories under the same product
  user-data root. Match complete components:
  `<root>/Doubao[Work]/User Data/sandbox_runtime/bases/<pack>` and
  `<root>/Doubao[Work]/User Data/<profile>/sandbox_envs_dir/envs/<session>`.
  No default application installation path is consulted. Arbitrarily renamed
  product data roots are not recognized by this rule.
- Conflicting valid WorkBuddy and Doubao markers retain unknown and add a
  diagnostic warning. Missing, malformed, or mismatched marker pairs do not
  match. Merely having a client installed is insufficient.

Fallback emits `windows_runtime_environment`, confidence `low`, and rule
`client.workbuddy.environment`, `client.doubao.environment`, or
`client.doubao_work.environment`. It does not claim a matched process or
signature. Existing ancestry warnings are retained in the diagnostic JSON.
Source metadata does not control authentication or deployment environment.

## Validation

Windows tests cover custom/Unicode/space paths, Git Bash paths, both Doubao
products, missing or malformed metadata, conflicting markers, ancestry
precedence, helper integration, and outgoing header mapping.

For a read-only smoke test in each client's normal tool entry:
`<test-binary> --internal-invocation-inspect 32`.
Run three times without changing sandbox permissions. Expected: WorkBuddy
fallback when ancestry is broken; existing signed Doubao matches remain high.
Using depth 1 deliberately truncates ancestry and tests fallback in isolation;
it is a diagnostic simulation, not proof of the client's normal process chain.
No login, business requests, global CLI replacement, or uploads are required.
