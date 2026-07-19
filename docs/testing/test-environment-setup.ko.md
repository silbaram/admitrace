# 테스트 환경 설정

[English](test-environment-setup.md) | 한국어

이 문서는 AdmiTrace 변경을 세 단계로 검증하는 환경을 준비합니다.

1. 클러스터가 필요 없는 오프라인 회귀 검사
2. 정확한 Kubernetes `1.36.2` API server를 사용하는 envtest parity·conformance
3. Docker와 kind에서 실제 Webhook transport까지 확인하는 선택적 end-to-end 검사

> [!IMPORTANT]
> AdmiTrace의 `called`는 지원하는 routing pipeline에서 Webhook이 호출 대상으로
> 선택되었다는 뜻입니다. 실제 HTTP/TLS 호출, Webhook response, allow/deny, patch와
> reinvocation은 AdmiTrace 결과에 포함되지 않습니다.

## 어떤 환경을 사용해야 하나

| 확인 목적 | 권장 환경 | 실제 API server | 실제 Webhook 호출 |
| --- | --- | --- | --- |
| 빠른 개발 회귀 | 로컬 Go test와 standalone CLI | 아니요 | 아니요 |
| 릴리스 parity·conformance | Kubernetes `1.36.2` envtest | 예 | test-only recorder 사용 |
| 배포 환경과 유사한 end-to-end | Docker + kind `1.36.2` | 예 | 별도 Webhook 배포 시 예 |

저장소의 릴리스 근거를 재현하려면 envtest를 우선 사용합니다. Docker+kind는 실제
Webhook 서버, TLS, Service와 `WebhookConfiguration`을 함께 배포해 transport까지
검사할 때 추가합니다. Docker Desktop 내장 Kubernetes는 patch version을 정확히
고정하기 어려우므로 이 프로젝트의 parity 근거로 사용하지 않습니다.

## 공통 준비

저장소 root에서 다음 버전을 확인합니다.

```sh
go version
```

필수 Go 버전은 다음과 같습니다.

```text
go version go1.26.5 ...
```

오프라인 기본 검사를 먼저 실행합니다.

```sh
go test -count=1 ./...
go vet ./...
./hack/verify-dependencies.sh
```

standalone CLI도 새로 빌드합니다.

```sh
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace version
./build/admitrace test docs/examples
./build/admitrace test validation/beta/scenarios
```

각 명령이 종료 코드 `0`을 반환해야 다음 단계로 진행합니다.

## envtest: 권장 parity·conformance 환경

envtest는 로컬에서 `kube-apiserver`와 `etcd`를 실행합니다. Docker cluster가 없어도
실제 Kubernetes API server의 defaulting, validation, discovery, RBAC와 admission
routing을 관찰할 수 있습니다. AdmiTrace conformance harness는 다음을 정확히
고정합니다.

- Kubernetes control-plane assets `1.36.2`
- `sigs.k8s.io/controller-runtime` `v0.24.1`
- Kubernetes Go modules `v0.36.2`

### 1. setup-envtest 설치

네트워크 연결이 가능한 환경에서 pinned 도구를 설치합니다.

```sh
go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.24.1
```

### 2. Kubernetes 1.36.2 assets 준비

저장소의 `.tools/`는 Git에서 제외됩니다. assets를 이 경로에 내려받고 출력된
절대 경로를 `KUBEBUILDER_ASSETS`로 설정합니다.

```sh
mkdir -p ./.tools/envtest
ENVTEST_TOOL="$(go env GOPATH)/bin/setup-envtest"
KUBEBUILDER_ASSETS="$("$ENVTEST_TOOL" use 1.36.2 --bin-dir ./.tools/envtest -p path)"
export KUBEBUILDER_ASSETS
```

`1.36.2` assets를 구할 수 없으면 다른 patch version으로 대체하지 말고 여기서
중단합니다. 다른 버전은 `kubernetes-1.36.2-defaults` profile의 parity 근거가
아닙니다.

### 3. assets 검증

세 실행 파일이 모두 존재해야 합니다.

```sh
test -x "$KUBEBUILDER_ASSETS/kube-apiserver"
test -x "$KUBEBUILDER_ASSETS/etcd"
test -x "$KUBEBUILDER_ASSETS/kubectl"
"$KUBEBUILDER_ASSETS/kube-apiserver" --version
```

마지막 명령은 정확히 다음 버전을 보고해야 합니다.

```text
Kubernetes v1.36.2
```

conformance 명령은 다운로드를 수행하지 않으므로 root module과 nested conformance
module의 dependency를 미리 준비합니다.

```sh
go mod download
go -C conformance mod download
```

### 4. 단계별 검증

먼저 전체 conformance suite를 실행합니다.

```sh
./hack/test-conformance.sh
```

공개 Gatekeeper·Istio 사례를 fresh CLI 결과와 Kubernetes oracle 양쪽에서
검증합니다.

```sh
./hack/test-beta-validation.sh
```

결정론적 parity report도 생성할 수 있습니다.

```sh
PARITY_REPORT=/tmp/admitrace-parity.json ./hack/test-parity-gate.sh
```

### 5. 전체 릴리스 gate

마지막으로 root test, vet, dependency boundary, fuzz, standalone smoke, parity,
conformance와 beta 검증을 한 번에 실행합니다.

```sh
GO_BIN="$(command -v go)" ./hack/verify-release-readiness.sh
```

성공 시 마지막에 다음 메시지가 출력됩니다.

```text
release readiness: passed
```

## Docker + kind: 선택적 end-to-end 환경

kind는 Docker container를 Kubernetes node로 사용합니다. 실제 API server에
`kubectl --dry-run=server` 요청을 보내고 Webhook Pod 로그까지 비교하려면 이
환경을 사용합니다.

> [!WARNING]
> kind cluster만 생성해서는 실제 Webhook 호출을 검증할 수 없습니다. 호출할
> Webhook 서버, TLS certificate, Service와 유효한 Validating 또는
> MutatingWebhookConfiguration을 cluster 안에 별도로 배포해야 합니다.

### 1. Docker, kind, kubectl 준비

macOS에서는 Docker Desktop을 먼저 실행합니다. Docker client와 server가 모두
응답하는지 확인합니다.

```sh
docker version
```

kind와 kubectl이 없다면 설치합니다.

```sh
brew install kind kubectl
kind version
kubectl version --client
```

Kubernetes node image를 직접 빌드하는 macOS·Windows 환경은 Docker VM에 최소
6 GiB, 권장 8 GiB memory를 할당합니다.

### 2. 정확한 1.36.2 node image 빌드

kind `v0.32.0` release의 prebuilt image는 Kubernetes `1.36.1`입니다. AdmiTrace
live hydration은 `1.36.1`을 거부하므로 upstream `v1.36.2` release에서 node
image를 직접 빌드합니다.

```sh
kind build node-image \
  --type release \
  --image admitrace/kindest-node:v1.36.2 \
  v1.36.2
```

이미지 정보를 확인합니다.

```sh
docker image inspect admitrace/kindest-node:v1.36.2
```

공식 prebuilt `v1.36.2` image가 이후 제공되더라도 tag만 사용하지 말고 해당 kind
release note에 게시된 digest까지 고정합니다.

### 3. 전용 cluster 생성

운영 context와 혼동하지 않도록 전용 이름을 사용합니다.

```sh
kind create cluster \
  --name admitrace-e2e \
  --image admitrace/kindest-node:v1.36.2
```

별도 kubeconfig 파일을 만들어 모든 명령에서 명시적으로 사용합니다.

```sh
mkdir -p ./.tools
kind get kubeconfig --name admitrace-e2e > ./.tools/kind-admitrace-e2e.kubeconfig
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  version
```

Server Version이 정확히 `v1.36.2`인지 확인합니다.

### 4. 실제 Webhook 배포

테스트 대상 Webhook 배포에는 최소 다음 항목이 필요합니다.

- Webhook server Deployment 또는 Pod
- API server가 접근할 Kubernetes Service
- Service DNS와 일치하는 TLS server certificate
- certificate를 검증할 `clientConfig.caBundle`
- `sideEffects: None` 또는 dry-run을 지원하는 `NoneOnDryRun`
- 지원하는 `admissionReviewVersions`
- 테스트 목적에 맞는 rules, selectors와 `matchConditions`

저장소의 `docs/manifest-examples/webhooks.yaml`은 오프라인 adapter 예제이며 실제
Webhook server와 완전한 transport 설정을 제공하지 않습니다. 그 파일을 그대로
cluster에 적용해서 end-to-end transport를 검사하지 않습니다.

Webhook 준비 상태와 설정을 확인합니다.

```sh
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  get validatingwebhookconfigurations,mutatingwebhookconfigurations
```

### 5. AdmiTrace live hydration 실행

실제로 생성하지 않을 resource YAML을 준비한 뒤 명시적 context와 kubeconfig로
실행합니다.

```sh
./build/admitrace --output json explain \
  --resource /absolute/path/to/resource.yaml \
  --context kind-admitrace-e2e \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  > /tmp/admitrace-kind-result.json
```

AdmiTrace hydration은 version, discovery, WebhookConfiguration LIST와 필요한
Namespace GET만 수행합니다. `called` 또는 `skipped`, terminal `reasonCode`와
`contextCompleteness`를 확인합니다.

### 6. 실제 API server 결과와 비교

같은 resource를 server-side dry-run으로 제출합니다.

```sh
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  apply --dry-run=server \
  -f /absolute/path/to/resource.yaml
```

dry-run request도 실제 Webhook transport를 수행할 수 있습니다. 테스트 cluster만
사용하고 Webhook의 `sideEffects`가 `None` 또는 `NoneOnDryRun`인지 먼저
확인합니다.

Webhook 로그를 확인합니다.

```sh
kubectl \
  --kubeconfig "$PWD/.tools/kind-admitrace-e2e.kubeconfig" \
  --context kind-admitrace-e2e \
  logs -n WEBHOOK_NAMESPACE deploy/WEBHOOK_DEPLOYMENT
```

비교 기준은 다음과 같습니다.

| AdmiTrace | API server·Webhook 관찰 |
| --- | --- |
| `called` | 해당 Webhook에 AdmissionReview 요청이 도달하는지 |
| `skipped` | 해당 Webhook에 요청이 도달하지 않는지 |
| `rejected-before-call` | Webhook transport 전에 요청이 거부되는지 |
| `indeterminate` | 부족한 Namespace, identity 또는 fixture를 보완할 수 있는지 |

Webhook이 실제로 요청을 받았더라도 response allow/deny, patch와 reinvocation은
AdmiTrace 결과가 아니라 API server 응답과 Webhook 로그에서 별도로 확인합니다.

### 7. cluster 정리

테스트가 끝나면 전용 cluster만 삭제합니다.

```sh
kind delete cluster --name admitrace-e2e
```

`.tools/kind-admitrace-e2e.kubeconfig`는 삭제된 cluster에만 연결되며 `.tools/`는
Git에서 제외됩니다.

## 기존 외부 cluster를 사용할 때

kind 대신 기존 cluster를 사용한다면 운영 cluster가 아닌 전용 테스트 cluster를
선택합니다. 먼저 server version을 확인합니다.

```sh
kubectl \
  --kubeconfig /absolute/path/to/kubeconfig \
  --context TEST_CONTEXT \
  version
```

정확한 `v1.36.2`가 아니면 AdmiTrace compatibility profile과 맞지 않으므로 live
hydration을 진행하지 않습니다. context 이름과 kubeconfig 경로는 AdmiTrace와
kubectl 명령에 항상 명시합니다.

## 자주 발생하는 문제

| 증상 | 확인할 내용 |
| --- | --- |
| `Cannot connect to the Docker daemon` | Docker Desktop 실행 여부와 `docker version`의 Server 항목 |
| server version mismatch | cluster가 정확히 Kubernetes `v1.36.2`인지 |
| `KUBEBUILDER_ASSETS` 오류 | `kube-apiserver`, `etcd`, `kubectl` 실행 권한과 asset version |
| dry-run이 Webhook 전에 거부됨 | `sideEffects`가 `None` 또는 `NoneOnDryRun`인지 |
| AdmiTrace는 `called`지만 Webhook 로그가 없음 | Service DNS, endpoint, TLS, CA bundle, timeout과 AdmissionReview negotiation |
| 오프라인 CRD가 `unsupported` | exact-version context discovery를 명시했는지 |

`called`와 실제 transport 관찰이 다르면 AdmiTrace trace의 selector·rule·CEL
결과뿐 아니라 Service, TLS와 API server event도 함께 조사합니다. Transport는
AdmiTrace 지원 범위 밖이므로 이 차이가 곧 routing evaluator 오류를 뜻하지는
않습니다.

## 공식 자료

- [kind Quick Start](https://kind.sigs.k8s.io/docs/user/quick-start/)
- [kind releases](https://github.com/kubernetes-sigs/kind/releases)
- [Kubernetes v1.36.2 release](https://github.com/kubernetes/kubernetes/releases/tag/v1.36.2)
- [Kubebuilder envtest 설정](https://book.kubebuilder.io/reference/envtest.html)
- [Kubernetes dynamic admission control](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
