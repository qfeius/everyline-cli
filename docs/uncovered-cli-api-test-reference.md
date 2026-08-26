# Everyline CLI V3 未覆盖接口调用入参与响应参考

## 1. 文档范围

本文依据《Everyline CLI V3 测试用例》与当前 CLI operation 目录进行差异整理，覆盖原测试用例尚未触达的 19 个远端接口：

- URL 合同上传：1 个。
- Checklist：7 个。
- Rule Group：4 个。
- Rule：7 个。

本文描述的是当前 CLI 实际发送的请求。所有路径均为相对路径，运行时会拼接 `--profile` 对应身份的 `base_url`。

推荐公共参数：

```bash
--profile test --as app --output json
```

示例中的资源 ID 均为占位值，应替换为测试环境实际返回值：

```text
<GROUP_ID>  规则分组 ID
<RULE_ID>   审查规则 ID
<CHECK_ID>  审查清单 ID
```

## 2. 响应与错误约定

普通业务接口以远端 `code=200` 为成功。远端成功响应形式为：

```json
{
  "code": 200,
  "msg": "success",
  "data": {}
}
```

CLI 会移除外层 envelope，只把 `data` 原样写入 stdout。例如远端返回：

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "id": "resource-1",
    "name": "示例资源"
  }
}
```

CLI stdout 为：

```json
{
  "id": "resource-1",
  "name": "示例资源"
}
```

当前仓库没有 Checklist、Rule Group、Rule 的字段级响应 Schema，因此本文中的成功响应示例只用于说明 CLI 的透传结果，不将示例字段定义为服务端固定响应契约。验收时应至少检查退出码为 `0`、stdout 是合法 JSON、需要串联的资源 ID 可以从实际 `data` 中取得。

远端失败时 stdout 不输出成功对象，stderr 保留：

```text
远端 API 错误: http=<HTTP_STATUS> code=<REMOTE_CODE> message=<REMOTE_MESSAGE> request_id=<REQUEST_ID>
```

退出码：

| 退出码 | 含义 |
|---:|---|
| `0` | 成功 |
| `2` | 命令参数、JSON 或本地 Schema 校验失败 |
| `3` | Profile 或鉴权失败 |
| `4` | 远端 API、运行时契约不匹配或接口能力未开放 |
| `5` | 网络错误、取消或超时 |

## 3. 未覆盖接口总表

| # | Operation ID | CLI 命令 | 方法与路径 | 当前真实调用状态 |
|---:|---|---|---|---|
| 1 | `uploadContractFileByURLV3` | `review file upload-url` | `POST /open-apis/contract-review/v1/file/contract/uploadByUrl` | 支持 |
| 2 | `createReviewChecklist` | `checklist create` | `POST /open-apis/review-rules/review-checklists` | 支持 |
| 3 | `batchCreateReviewChecklists` | `checklist batch-create` | `POST /open-apis/review-rules/review-checklists/batch` | 仅 dry-run |
| 4 | `listReviewChecklists` | `checklist list` | `GET /open-apis/review-rules/review-checklists` | 支持 |
| 5 | `updateReviewChecklist` | `checklist update` | `PUT /open-apis/review-rules/review-checklists/{id}` | 支持 |
| 6 | `batchUpdateReviewChecklists` | `checklist batch-update` | `PUT /open-apis/review-rules/review-checklists/batch` | 仅 dry-run |
| 7 | `deleteReviewChecklist` | `checklist delete` | `DELETE /open-apis/review-rules/review-checklists/{id}` | 支持，需 `--yes` |
| 8 | `batchDeleteReviewChecklists` | `checklist batch-delete` | `DELETE /open-apis/review-rules/review-checklists/batch` | 支持，需 `--yes` |
| 9 | `createReviewRuleGroup` | `rule group create` | `POST /open-apis/review-rules/review-rule-groups` | 支持 |
| 10 | `listReviewRuleGroups` | `rule group list` | `GET /open-apis/review-rules/review-rule-groups` | 支持 |
| 11 | `updateReviewRuleGroup` | `rule group update` | `PUT /open-apis/review-rules/review-rule-groups/{id}` | 支持 |
| 12 | `deleteReviewRuleGroup` | `rule group delete` | `DELETE /open-apis/review-rules/review-rule-groups/{id}` | 支持，需 `--yes`，服务端固定级联 |
| 13 | `createReviewRule` | `rule create` | `POST /open-apis/review-rules/review-rule-groups/{groupId}/rules` | 支持 |
| 14 | `batchCreateReviewRules` | `rule batch-create` | `POST /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch` | 仅 dry-run |
| 15 | `listReviewRules` | `rule list` | `GET /open-apis/review-rules/review-rule-groups/{groupId}/rules` | 支持 |
| 16 | `updateReviewRule` | `rule update` | `PUT /open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}` | 支持 |
| 17 | `batchUpdateReviewRules` | `rule batch-update` | `PUT /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch` | 仅 dry-run |
| 18 | `deleteReviewRule` | `rule delete` | `DELETE /open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}` | 支持，需 `--yes` |
| 19 | `batchDeleteReviewRules` | `rule batch-delete` | `DELETE /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch` | 支持，需 `--yes` |

## 4. URL 合同上传

### 4.1 `uploadContractFileByURLV3`

HTTP：

```text
POST /open-apis/contract-review/v1/file/contract/uploadByUrl
Content-Type: application/json
```

入参：

| CLI 参数 | HTTP JSON 字段 | 必填 | 约束 |
|---|---|---|---|
| `--file-url` | `fileUrl` | 是 | 完整的 `http` 或 `https` URL |
| `--name` | `fileName` | 是 | 以 `.doc`、`.docx` 或 `.pdf` 结尾 |

CLI 调用：

```bash
everyline-cli review file upload-url \
  --profile test --as app \
  --file-url 'https://files.example.com/采购合同.pdf' \
  --name '采购合同.pdf' \
  --output json
```

实际 HTTP 请求体：

```json
{
  "fileUrl": "https://files.example.com/采购合同.pdf",
  "fileName": "采购合同.pdf"
}
```

成功结果：退出码 `0`，stdout 为服务端 `data`。后续工作流至少需要能从实际结果中读取 `fileId`。代表性结果：

```json
{
  "fileId": 997024795
}
```

dry-run：

```bash
everyline-cli review file upload-url \
  --file-url 'https://files.example.com/采购合同.pdf' \
  --name '采购合同.pdf' \
  --dry-run --output json
```

dry-run stdout：

```json
{
  "fileUrl": "https://files.example.com/采购合同.pdf",
  "fileName": "采购合同.pdf"
}
```

建议至少补充以下场景：成功上传、非法 URL、非法扩展名、请求体不含 `channelType`、V1 实际路径，以及 `review run` 的 URL 来源链路。

## 5. Checklist 接口

### 5.1 Checklist 公共请求模型

```json
{
  "id": "<CHECK_ID>",
  "name": "采购合同审查清单",
  "contractCategory": ["采购合同"],
  "reviewStage": [1],
  "enabled": true,
  "reviewRuleIds": ["<RULE_ID>"]
}
```

| 字段 | 类型 | 创建/单项更新 | 批量更新 | 约束 |
|---|---|---|---|---|
| `id` | string | 可选；单项更新若提供必须与路径 ID 一致 | 必填 | 非空 |
| `name` | string | 必填 | 必填 | 非空 |
| `contractCategory` | string[] | 可选 | 可选 | 提供时至少一项 |
| `reviewStage` | integer[] | 可选 | 可选 | 提供时至少一项 |
| `enabled` | boolean | 可选 | 可选 | 显式 `false` 会保留 |
| `reviewRuleIds` | string[] | 必填 | 必填 | 至少一个非空规则 ID |

### 5.2 `createReviewChecklist`

HTTP：`POST /open-apis/review-rules/review-checklists`

CLI 调用：

```bash
everyline-cli checklist create \
  --profile test --as app \
  --data '{"name":"采购合同审查清单","contractCategory":["采购合同"],"reviewStage":[1],"enabled":true,"reviewRuleIds":["<RULE_ID>"]}' \
  --output json
```

HTTP 请求体与 `--data` 对象一致。成功结果为服务端 `data` 原样输出；若服务端返回资源对象，代表性 stdout 为：

```json
{
  "id": "<CHECK_ID>",
  "name": "采购合同审查清单",
  "reviewRuleIds": ["<RULE_ID>"]
}
```

### 5.3 `batchCreateReviewChecklists`

定义的 HTTP：`POST /open-apis/review-rules/review-checklists/batch`

当前 CLI 只允许 dry-run：

```bash
everyline-cli checklist batch-create \
  --data '[{"name":"采购合同审查清单","reviewRuleIds":["<RULE_ID>"]},{"name":"销售合同审查清单","reviewRuleIds":["<RULE_ID>"]}]' \
  --dry-run --output json
```

dry-run stdout 为规范化后的 Checklist 数组：

```json
[
  {
    "name": "采购合同审查清单",
    "reviewRuleIds": ["<RULE_ID>"]
  },
  {
    "name": "销售合同审查清单",
    "reviewRuleIds": ["<RULE_ID>"]
  }
]
```

不带 `--dry-run` 时不会发送 HTTP 请求，退出码为 `4`，stderr 包含：

```text
接口契约尚未核验: batchCreateReviewChecklists；请先补齐接口详情页中的请求体定义
```

### 5.4 `listReviewChecklists`

HTTP：`GET /open-apis/review-rules/review-checklists`

CLI 调用：

```bash
everyline-cli checklist list \
  --profile test --as app \
  --name '采购合同审查清单' \
  --enabled=true \
  --page-index 1 --page-size 20 \
  --output json
```

Query 参数：

| CLI 参数 | HTTP Query | 约束 |
|---|---|---|
| `--name` | `name` | 可选 string |
| `--review-stage` | 重复 `reviewStages` | 可选 string[] |
| `--contract-category` | 重复 `contractCategories` | 可选 string[] |
| `--start-time` | `startTime` | Unix 毫秒，非负 |
| `--end-time` | `endTime` | Unix 毫秒，非负且不早于 startTime |
| `--enabled` | `enabled` | 只有显式传 flag 才发送 |
| `--create-employee-id` | 重复 `createEmployeeIds` | 可选 string[] |
| `--update-employee-id` | 重复 `updateEmployeeIds` | 可选 string[] |
| `--sort` | 重复 `sort` | 可选排序条件 |
| `--page-index` | `pageIndex` | 大于 0 时发送 |
| `--page-size` | `pageSize` | 大于 0 时发送 |

成功结果为列表接口的 `data` 原样输出。代表性 stdout：

```json
{
  "items": [
    {
      "id": "<CHECK_ID>",
      "name": "采购合同审查清单"
    }
  ],
  "pageIndex": 1,
  "pageSize": 20,
  "total": 1
}
```

`items/pageIndex/pageSize/total` 仅为示例字段，测试应以 test 环境实际 `data` 为准。

### 5.5 `updateReviewChecklist`

HTTP：`PUT /open-apis/review-rules/review-checklists/{id}`

CLI 调用：

```bash
everyline-cli checklist update \
  --profile test --as app \
  --id '<CHECK_ID>' \
  --data '{"name":"采购合同审查清单-更新","enabled":false,"reviewRuleIds":["<RULE_ID>"]}' \
  --output json
```

实际 HTTP 请求体：

```json
{
  "name": "采购合同审查清单-更新",
  "enabled": false,
  "reviewRuleIds": ["<RULE_ID>"]
}
```

单项更新的资源 ID 只放在路径中；即使 body 提供相同 `id`，CLI 也会在发送前移除。body ID 与 `--id` 不一致时返回退出码 `2`。

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "id": "<CHECK_ID>",
  "name": "采购合同审查清单-更新",
  "enabled": false
}
```

### 5.6 `batchUpdateReviewChecklists`

定义的 HTTP：`PUT /open-apis/review-rules/review-checklists/batch`

当前 CLI 只允许 dry-run，且每项必须包含 `id`：

```bash
everyline-cli checklist batch-update \
  --data '[{"id":"<CHECK_ID>","name":"采购合同审查清单-批量更新","reviewRuleIds":["<RULE_ID>"]}]' \
  --dry-run --output json
```

dry-run stdout：

```json
[
  {
    "id": "<CHECK_ID>",
    "name": "采购合同审查清单-批量更新",
    "reviewRuleIds": ["<RULE_ID>"]
  }
]
```

真实调用不会发送 HTTP 请求，退出码为 `4`，stderr 包含：

```text
接口契约尚未核验: batchUpdateReviewChecklists；请先补齐接口详情页中的请求体定义
```

### 5.7 `deleteReviewChecklist`

HTTP：`DELETE /open-apis/review-rules/review-checklists/{id}`，无请求体。

CLI 调用：

```bash
everyline-cli checklist delete \
  --profile test --as app \
  --id '<CHECK_ID>' --yes \
  --output json
```

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "success": true
}
```

省略 `--yes` 时不会发送 HTTP 请求，退出码为 `2`，stderr 为 `删除操作必须显式传 --yes`。

### 5.8 `batchDeleteReviewChecklists`

HTTP：`DELETE /open-apis/review-rules/review-checklists/batch`

CLI 调用：

```bash
everyline-cli checklist batch-delete \
  --profile test --as app \
  --id '<CHECK_ID>' --id '<ANOTHER_CHECK_ID>' \
  --yes --output json
```

也可以通过 `--input` 或 `--data` 提供 ID 数组，但 ID flags 和 JSON 输入只能二选一：

```bash
everyline-cli checklist batch-delete \
  --data '["<CHECK_ID>","<ANOTHER_CHECK_ID>"]' \
  --yes --output json
```

实际 HTTP 请求体：

```json
{
  "ids": ["<CHECK_ID>", "<ANOTHER_CHECK_ID>"]
}
```

ID 会去除首尾空白；空数组、空 ID 或重复 ID 会在契约阶段被拒绝。成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "success": true
}
```

## 6. Rule Group 接口

### 6.1 Rule Group 公共请求模型

```json
{
  "id": "<GROUP_ID>",
  "name": "付款规则分组",
  "sourceType": 0
}
```

| 字段 | 类型 | 要求 |
|---|---|---|
| `id` | string | 单项更新可选；若提供必须与路径 ID 一致 |
| `name` | string | 必填、非空 |
| `sourceType` | integer | 可选、不能小于 0 |

### 6.2 `createReviewRuleGroup`

HTTP：`POST /open-apis/review-rules/review-rule-groups`

CLI 调用：

```bash
everyline-cli rule group create \
  --profile test --as app \
  --data '{"name":"付款规则分组","sourceType":0}' \
  --output json
```

HTTP 请求体与 `--data` 对象一致。成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "id": "<GROUP_ID>",
  "name": "付款规则分组",
  "sourceType": 0
}
```

### 6.3 `listReviewRuleGroups`

HTTP：`GET /open-apis/review-rules/review-rule-groups`

CLI 调用：

```bash
everyline-cli rule group list \
  --profile test --as app \
  --page-index 1 --page-size 20 \
  --output json
```

可选 Query：重复 `sort`、正整数 `pageIndex`、正整数 `pageSize`。

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "items": [
    {
      "id": "<GROUP_ID>",
      "name": "付款规则分组"
    }
  ],
  "pageIndex": 1,
  "pageSize": 20,
  "total": 1
}
```

列表字段仅为示例，测试应保留并记录实际 `data`。

### 6.4 `updateReviewRuleGroup`

HTTP：`PUT /open-apis/review-rules/review-rule-groups/{id}`

CLI 调用：

```bash
everyline-cli rule group update \
  --profile test --as app \
  --id '<GROUP_ID>' \
  --data '{"name":"付款规则分组-更新","sourceType":0}' \
  --output json
```

实际 HTTP 请求体不包含 `id`：

```json
{
  "name": "付款规则分组-更新",
  "sourceType": 0
}
```

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "id": "<GROUP_ID>",
  "name": "付款规则分组-更新",
  "sourceType": 0
}
```

### 6.5 `deleteReviewRuleGroup`

HTTP：`DELETE /open-apis/review-rules/review-rule-groups/{id}`，无请求体。服务端固定级联删除分组内规则。

CLI 调用：

```bash
everyline-cli rule group delete \
  --profile test --as app \
  --id '<GROUP_ID>' --yes \
  --output json
```

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "success": true
}
```

省略 `--yes` 时不会调用远端，退出码为 `2`，stderr 为 `级联删除规则分组必须显式传 --yes`。

## 7. Rule 接口

### 7.1 Rule 公共请求模型

```json
{
  "id": "<RULE_ID>",
  "name": "付款期限规则",
  "riskLevel": 2,
  "riskTips": "付款期限过长",
  "content": "付款期限不得超过约定上限",
  "sourceType": 0
}
```

| 字段 | 类型 | 要求 |
|---|---|---|
| `id` | string | 批量更新必填；单项更新可选且必须与 `--rule-id` 一致 |
| `name` | string | 必填、非空 |
| `riskLevel` | integer | 必填、不能小于 0；`0` 是有效值 |
| `riskTips` | string | 可选 |
| `content` | string | 必填、非空 |
| `sourceType` | integer | 可选、不能小于 0；`0` 是有效值 |

所有 Rule 接口都要求非空 `--group-id`，所属分组只通过 URL 路径传输。

### 7.2 `createReviewRule`

HTTP：`POST /open-apis/review-rules/review-rule-groups/{groupId}/rules`

CLI 调用：

```bash
everyline-cli rule create \
  --profile test --as app \
  --group-id '<GROUP_ID>' \
  --data '{"name":"付款期限规则","riskLevel":2,"riskTips":"付款期限过长","content":"付款期限不得超过约定上限","sourceType":0}' \
  --output json
```

实际 HTTP 请求体只包含 Rule 对象，不包含 `groupId`。成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "id": "<RULE_ID>",
  "name": "付款期限规则",
  "riskLevel": 2,
  "content": "付款期限不得超过约定上限"
}
```

### 7.3 `batchCreateReviewRules`

定义的 HTTP：`POST /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch`

当前 CLI 只允许 dry-run：

```bash
everyline-cli rule batch-create \
  --group-id '<GROUP_ID>' \
  --data '[{"name":"付款期限规则","riskLevel":2,"content":"付款期限不得超过约定上限"},{"name":"违约责任规则","riskLevel":1,"content":"违约责任应当明确"}]' \
  --dry-run --output json
```

dry-run stdout：

```json
{
  "groupId": "<GROUP_ID>",
  "data": [
    {
      "name": "付款期限规则",
      "riskLevel": 2,
      "content": "付款期限不得超过约定上限"
    },
    {
      "name": "违约责任规则",
      "riskLevel": 1,
      "content": "违约责任应当明确"
    }
  ]
}
```

真实调用不会发送 HTTP 请求，退出码为 `4`，stderr 包含：

```text
接口契约尚未核验: batchCreateReviewRules；请先补齐接口详情页中的请求体定义
```

### 7.4 `listReviewRules`

HTTP：`GET /open-apis/review-rules/review-rule-groups/{groupId}/rules`

CLI 调用：

```bash
everyline-cli rule list \
  --profile test --as app \
  --group-id '<GROUP_ID>' \
  --page-index 1 --page-size 20 \
  --output json
```

可选 Query：重复 `sort`、正整数 `pageIndex`、正整数 `pageSize`。

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "items": [
    {
      "id": "<RULE_ID>",
      "name": "付款期限规则",
      "riskLevel": 2
    }
  ],
  "pageIndex": 1,
  "pageSize": 20,
  "total": 1
}
```

列表字段仅为示例，测试应保留并记录实际 `data`。

### 7.5 `updateReviewRule`

HTTP：`PUT /open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}`

CLI 调用：

```bash
everyline-cli rule update \
  --profile test --as app \
  --group-id '<GROUP_ID>' --rule-id '<RULE_ID>' \
  --data '{"name":"付款期限规则-更新","riskLevel":2,"riskTips":"付款期限超过约定上限","content":"付款期限不得超过 60 天","sourceType":0}' \
  --output json
```

实际 HTTP 请求体不包含 `groupId`、`ruleId` 或 body `id`：

```json
{
  "name": "付款期限规则-更新",
  "riskLevel": 2,
  "riskTips": "付款期限超过约定上限",
  "content": "付款期限不得超过 60 天",
  "sourceType": 0
}
```

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "id": "<RULE_ID>",
  "name": "付款期限规则-更新",
  "riskLevel": 2,
  "content": "付款期限不得超过 60 天"
}
```

### 7.6 `batchUpdateReviewRules`

定义的 HTTP：`PUT /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch`

当前 CLI 只允许 dry-run，数组每项必须包含 `id`：

```bash
everyline-cli rule batch-update \
  --group-id '<GROUP_ID>' \
  --data '[{"id":"<RULE_ID>","name":"付款期限规则-批量更新","riskLevel":2,"content":"付款期限不得超过 60 天"}]' \
  --dry-run --output json
```

dry-run stdout：

```json
{
  "groupId": "<GROUP_ID>",
  "data": [
    {
      "id": "<RULE_ID>",
      "name": "付款期限规则-批量更新",
      "riskLevel": 2,
      "content": "付款期限不得超过 60 天"
    }
  ]
}
```

真实调用不会发送 HTTP 请求，退出码为 `4`，stderr 包含：

```text
接口契约尚未核验: batchUpdateReviewRules；请先补齐接口详情页中的请求体定义
```

### 7.7 `deleteReviewRule`

HTTP：`DELETE /open-apis/review-rules/review-rule-groups/{groupId}/rules/{ruleId}`，无请求体。

CLI 调用：

```bash
everyline-cli rule delete \
  --profile test --as app \
  --group-id '<GROUP_ID>' --rule-id '<RULE_ID>' \
  --yes --output json
```

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "success": true
}
```

省略 `--yes` 时不会调用远端，退出码为 `2`，stderr 为 `删除规则必须显式传 --yes`。

### 7.8 `batchDeleteReviewRules`

HTTP：`DELETE /open-apis/review-rules/review-rule-groups/{groupId}/rules/batch`

CLI 调用：

```bash
everyline-cli rule batch-delete \
  --profile test --as app \
  --group-id '<GROUP_ID>' \
  --id '<RULE_ID>' --id '<ANOTHER_RULE_ID>' \
  --yes --output json
```

也可以通过 JSON 数组提供规则 ID：

```bash
everyline-cli rule batch-delete \
  --group-id '<GROUP_ID>' \
  --data '["<RULE_ID>","<ANOTHER_RULE_ID>"]' \
  --yes --output json
```

实际 HTTP body 只包含 `ids`，`groupId` 只存在于路径：

```json
{
  "ids": ["<RULE_ID>", "<ANOTHER_RULE_ID>"]
}
```

成功结果为服务端 `data` 原样输出。代表性 stdout：

```json
{
  "success": true
}
```

## 8. 推荐测试执行顺序

为保证资源依赖和删除可回收，建议按以下顺序补测：

1. `review file upload-url`，记录实际 URL 上传响应。
2. `rule group create`，保存 `<GROUP_ID>`。
3. `rule create`，保存 `<RULE_ID>`。
4. `checklist create`，使用 `<RULE_ID>` 并保存 `<CHECK_ID>`。
5. 执行 Checklist、Rule Group、Rule 的 list 与 update。
6. 对四个未核验批量创建/更新命令执行 dry-run，并验证不带 dry-run 时退出码为 `4` 且未发出 HTTP 请求。
7. 测试 Checklist 和 Rule 的 batch-delete 时使用专门创建的可删除数据。
8. 先删除 Checklist，再删除 Rule，最后级联删除空的 Rule Group。

删除类测试必须使用隔离测试数据，并记录 `--yes` 缺失时本地拦截、提供 `--yes` 后远端成功两种结果。

## 9. 补测完成标准

- 19 个 operation 均有独立用例编号。
- 每条用例记录 CLI 版本、Profile、身份、完整命令、退出码、stdout、stderr 和远端 `request_id`。
- 所有写操作先执行 `--dry-run` 或 `--print-input`，确认规范化请求后再执行真实调用。
- 四个未核验批量创建/更新 operation 只验证 dry-run 和失败关闭，不把“未发送远端请求”误判为接口成功。
- URL 上传明确验证 V1 实际路径，并确认请求体不包含 `channelType`。
- 删除操作验证缺少 `--yes` 时不发送请求，并使用隔离测试资源执行真实删除。
- 成功 stdout 始终可被 JSON 解析；失败不在 stdout 伪造成功对象。
- Checklist、Rule Group、Rule 的实际 `data` 全量保存，后续获得正式 response schema 后再升级字段级断言。
