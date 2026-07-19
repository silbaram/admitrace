# 빠른 시작

[English](quickstart.md) | 한국어

AdmiTrace는 재생 가능한 `Scenario` 형식과 일반 Kubernetes resource를 모두 입력으로 받습니다. Raw resource mode는 `CREATE` routing을 평가하며, 사용자가 kubeconfig context를 명시하기 전까지 완전히 오프라인입니다.

## 빌드

저장소 루트에서 실행합니다.

```sh
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace version
```

## Resource를 완전 오프라인으로 설명

`-f/--file`은 universal input입니다. 단일 `admitrace.io/v1alpha1` `Scenario`는 기존 결과 schema를 그대로 사용하고, 그 밖의 파일·stdin stream·directory는 resource mode로 들어갑니다. `--resource`는 resource mode를 명시하는 동의어입니다.

Resource 하나를 명시적 WebhookConfiguration·Namespace 파일과 함께 설명합니다.

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

두 resource 문서와 두 configuration 문서의 조합을 canonical JSON으로 평가합니다.

```sh
./build/admitrace --output json explain \
  -f docs/manifest-examples/resources.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

같은 resource stream을 stdin으로 줄 수 있으며, resource 전용 directory는 YAML/JSON 파일을 lexical 순서로 처리합니다.

```sh
./build/admitrace explain -f - \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml \
  < docs/manifest-examples/resources.yaml

./build/admitrace explain -f docs/manifest-examples/resource-directory \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

Primary directory에는 resource만 있어야 합니다. 잘못된 문서는 논리 파일명과 1-based document index로 보고되며, complete로 오해할 partial output을 만들지 않습니다.

`--context`가 없으면 client 생성과 Kubernetes API 요청이 모두 0건입니다. 오프라인 GVK 해석은 생성된 Kubernetes `1.36.2` built-in catalog만 사용합니다. 알 수 없는 GVK·CRD의 plural이나 scope는 추측하지 않고 `unsupported`로 종료합니다.

## 제한적 hydration 명시 활성화

CRD discovery가 필요하거나 configuration·Namespace 파일이 없을 때 context 이름을 명시합니다.

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --context production \
  --kubeconfig /path/to/kubeconfig
```

Hydration은 먼저 `GET /version`을 수행하고 server가 정확히 `v1.36.2`일 때만 계속합니다. 다른 patch/minor와 vendor suffix는 거부합니다. 이후 surface도 GET-only입니다. Discovery, Validating/MutatingWebhookConfiguration LIST(HTTP GET), 필요한 Namespace GET만 허용하며 SubjectAccessReview, dry-run, watch, 변경 요청은 보내지 않습니다.

명시적 파일이 우선하며 해당 cluster read를 생략합니다. CRD discovery는 필요하지만 configuration LIST나 Namespace GET 권한이 없을 때 사용할 수 있습니다.

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --context production \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml
```

Kubeconfig credential은 Kubernetes API 연결에만 사용되며 admission request identity가 되지 않습니다. `matchConditions`가 `request.userInfo`를 사용한다면 identity를 명시합니다.

```sh
./build/admitrace explain \
  --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml \
  --user alice --group developers --user-extra tenant=blue
```

Configuration, Namespace, identity, equivalence, authorization 문맥이 부족하면 추측하지 않고 `indeterminate`·`unsupported`, 종료 코드 `3`, 필요한 file fallback 안내를 제공합니다.

## Snapshot export와 재생

`--snapshot-out`은 resource/configuration 조합별 canonical Scenario 하나를 존재하지 않거나 비어 있는 디렉터리에 기록합니다.

```sh
snapshot_dir=$(mktemp -d)
./build/admitrace --output json explain \
  --resource docs/manifest-examples/resource.yaml \
  --webhook-config docs/manifest-examples/webhooks.yaml \
  --namespace-object docs/manifest-examples/namespace.yaml \
  --user alice \
  --snapshot-out "$snapshot_dir"

./build/admitrace explain -f "$snapshot_dir/0001-0001.yaml"
```

SnapshotPolicy는 exact-copy-or-refuse입니다. core/v1 `Secret`은 항상 거부합니다. 명시적 `UserInfo`와 사용자가 제공한 일반 resource·CRD는 정확한 재생을 위해 변경 없이 저장하며, custom resource field의 field redaction이나 generic secret detection을 보장하지 않습니다. Kubeconfig bytes·credential·API server URL·context·자동 connection identity는 복사하지 않습니다. 게시된 directory/file mode는 `0700`/`0600`입니다.

## 기존 Scenario 동작 유지

기존 Scenario 명령은 그대로입니다.

```sh
./build/admitrace explain -f docs/examples/validating.yaml
./build/admitrace --output json explain -f docs/examples/mutating.yaml
./build/admitrace test docs/examples
```

`test`는 Scenario fixture만 재귀 탐색해 expectation을 비교하며 raw resource를 adapt하지 않습니다.

## 결과 읽기

Resource mode는 입력 resource마다 `admitrace.manifest-explanation/v1alpha1` envelope을 만들고 source/document provenance, exact profile, context completeness, diagnostics와 configuration별 기존 evaluator result를 순서대로 담습니다. Text·JSON은 결정론적입니다.

`called`는 Webhook이 routing에서 선택됐다는 뜻입니다. AdmissionReview HTTP/TLS 요청을 보내지 않으며 response, allow/deny, patch, reinvocation을 관찰하지 않습니다. 전체 계약과 종료 코드는 [Scenario·결과 레퍼런스](reference.ko.md)를 참고하세요.
