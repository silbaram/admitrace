# 빠른 시작

[English](quickstart.md) | 한국어

AdmiTrace는 제공된 request snapshot 하나를 Validating 또는 Mutating Webhook configuration 하나와 비교합니다. 오프라인으로 실행되며 클러스터가 필요하지 않습니다.

## 빌드

저장소 루트에서 실행합니다.

```sh
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace version
```

실행 가능한 예제 Scenario 두 개가 포함되어 있습니다.

- [`examples/validating.yaml`](examples/validating.yaml): Pod `CREATE`가 Validating Webhook 호출 대상으로 선택되는 예제
- [`examples/mutating.yaml`](examples/mutating.yaml): 초기 snapshot에서 ConfigMap `UPDATE`가 Mutating Webhook 호출 대상으로 선택되는 예제

## Scenario 설명

기본 text 출력은 사람이 순서대로 routing trace를 읽을 때 사용합니다.

```sh
./build/admitrace explain --file docs/examples/validating.yaml
```

도구에서 사용할 canonical JSON 출력은 배열 순서와 absent·empty 차이를 보존합니다.

```sh
./build/admitrace --output json explain --file docs/examples/validating.yaml
```

표준 입력에서는 Scenario 하나만 읽습니다.

```sh
./build/admitrace explain --file - < docs/examples/validating.yaml
```

정상 결과는 stdout, 사용법·잘못된 입력·내부 오류 진단은 stderr에 기록됩니다. 모든 Webhook이 완전히 판정되면 `explain`은 `0`, 하나라도 `indeterminate` 또는 `unsupported`이면 `3`으로 종료합니다.

## CI expectation 검사

`test`는 파일과 디렉터리를 받습니다. 디렉터리에서는 일반 `.yaml`, `.yml`, `.json` 파일을 재귀 탐색하고, 정리된 중복 경로를 제거한 뒤 lexical 순서로 평가합니다.

```sh
./build/admitrace test docs/examples
./build/admitrace --output json test docs/examples
```

두 예제의 expectation은 실제 결과와 일치하므로 종료 코드는 `0`입니다. determination, 명시한 outcome 또는 terminal reason이 다르면 `1`입니다. 정확히 기대한 `indeterminate`·`unsupported`는 `0`, expectation이 없는 incomplete 결과는 `3`입니다.

최소 CI 흐름은 다음과 같습니다.

```sh
set -eu
mkdir -p ./build
go build -o ./build/admitrace ./cmd/admitrace
./build/admitrace --output json test docs/examples
```

지원하는 report 형식은 text와 JSON뿐이며 JUnit XML은 생성하지 않습니다.

## 결과 읽기

각 Webhook 결과에는 다음 항목이 있습니다.

- `determination`: 지원 계약 안에서 평가를 완료했는지 표시합니다.
- 선택적 `outcome`: determinate 결과의 `called`, `skipped`, `rejected-before-call`입니다.
- `trace`: 안정적인 `reasonCode`와 `pending`, `discarded`, `terminal` 상태를 가진 순서 보존 단계입니다.
- `diagnostics`: missing context, unsupported capability, evaluation 정보를 구조화합니다.

`called`는 호출 대상으로 선택되었다는 의미입니다. 실제 HTTP/TLS 호출이나 Webhook 응답 평가는 없습니다. Mutating 결과는 초기 snapshot eligibility만 나타내며 patch와 reinvocation을 시뮬레이션하지 않습니다.

전체 계약, 종료 코드, reason code, 지원 정책과 비범위는 [Scenario·결과 레퍼런스](reference.ko.md)를 참고하세요.
