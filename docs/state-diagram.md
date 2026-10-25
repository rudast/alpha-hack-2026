# Диаграмма состояний заявки

Статусы взяты из CJM «Кредитный кабинет».

```mermaid
stateDiagram-v2
    [*] --> DRAFT
    DRAFT --> SUBMITTED: клиент отправил заявку
    SUBMITTED --> DATA_CHECK
    DATA_CHECK --> INFO_REQUESTED: не хватает данных
    INFO_REQUESTED --> DATA_CHECK: клиент дополнил данные
    DATA_CHECK --> SCORING
    SCORING --> MANUAL_REVIEW: сложный случай или негативный скоринг
    MANUAL_REVIEW --> APPROVED
    MANUAL_REVIEW --> COUNTER_OFFER
    MANUAL_REVIEW --> REJECTED
    MANUAL_REVIEW --> INFO_REQUESTED: сотрудник запросил документы
    INFO_REQUESTED --> MANUAL_REVIEW: клиент дополнил (в общий пул)
    SCORING --> APPROVED
    SCORING --> COUNTER_OFFER
    COUNTER_OFFER --> SIGNING: клиент принял
    COUNTER_OFFER --> DRAFT: клиент изменил параметры
    APPROVED --> SIGNING
    REJECTED --> DRAFT: подать заново
    SIGNING --> SIGNED
    SIGNED --> DISBURSED
    DISBURSED --> ACTIVE
    ACTIVE --> OVERDUE
    OVERDUE --> ACTIVE
    ACTIVE --> CLOSED
    CLOSED --> [*]
```

> Отказ (`REJECTED`) выносит только сотрудник: из `SCORING` прямого перехода в него нет, заявка с негативным скорингом сначала попадает в `MANUAL_REVIEW`.

> Переходы `COUNTER_OFFER → DRAFT` и `REJECTED → DRAFT` («Изменить» и «Подать заново») — наше допущение по этапу 05 CJM, его нужно подтвердить.
