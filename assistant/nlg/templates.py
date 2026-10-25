"""
Templates and formatting for the Credit Assistant Curator.
Businesslike, friendly, helpful tone of voice (Alfa-Bank style).
"""
from __future__ import annotations
from typing import List, Optional, Dict, Any
from assistant.domain.models import ProductOffer, ActionCommand, ProductSpec
from assistant.domain.catalog import get_all_products, get_product_by_id


def format_rubles(val: int) -> str:
    return f"{val:,}".replace(",", " ") + " ₽"


def format_term(months: int) -> str:
    years = months // 12
    rest = months % 12
    if years == 0:
        return f"{months} мес."
    if rest == 0:
        if years == 1:
            return "1 год"
        if 2 <= years <= 4:
            return f"{years} года"
        return f"{years} лет"
    return f"{years} г. {rest} мес."


def render_greeting(context_page: Optional[str] = None) -> Dict[str, Any]:
    if context_page == "cabinet":
        text = (
            "Привет! Я **Альфи** — ваш персональный куратор и бизнес-помощник в Альфа-Банке.\n\n"
            "Помогу проверить статус заявки, сориентироваться в условиях "
            "или подсказать, какие документы нужны банку."
        )
        quick_replies = ["Статус заявки", "Необходимые документы", "Что ты умеешь?", "Подобрать новый кредит"]
    else:
        text = (
            "Привет! Я **Альфи** — ваш персональный бизнес-помощник по кредитам в Альфа-Банке.\n\n"
            "Помогу быстро подобрать выгодную программу из 14 кредитных продуктов "
            "и подставить параметры в калькулятор.\n\n"
            "💬 **Вы можете написать мне:**\n"
            "• Сумму и цель: *«3 млн на маркетплейс»*, *«оборотный кредит на 1.5 млн»*, *«овердрафт для ИП»*\n"
            "• Вопрос по условиям: *«можно ли закрыть досрочно?»*, *«какой залог нужен?»*, *«сколько рассматривают заявку?»*"
        )
        quick_replies = [
            "3 млн на маркетплейсы",
            "Оборотные средства 1 млн",
            "Без залога на 1 год",
            "Что ты умеешь?",
            "Досрочное погашение без штрафов?",
        ]

    return {
        "reply": text,
        "recommended_products": [],
        "actions": [],
        "quick_replies": quick_replies,
        "dialogue_state": "GREETING",
    }


def render_collect_slots() -> Dict[str, Any]:
    text = (
        "С удовольствием помогу подобрать оптимальный кредит для вашей компании!\n\n"
        "Чтобы предложить наиболее выгодные условия, уточните, пожалуйста:\n"
        "• Какая сумма вам необходима?\n"
        "• На какой срок планируете финансирование?\n"
        "• Какая цель кредита (закупка товара, кассовый разрыв, маркетплейсы, авто или недвижимость)?"
    )
    quick_replies = [
        "1 млн на 1 год",
        "3 млн на маркетплейсы",
        "500 тыс. без залога",
        "Автокредит",
    ]
    return {
        "reply": text,
        "recommended_products": [],
        "actions": [],
        "quick_replies": quick_replies,
        "dialogue_state": "COLLECTING_SLOTS",
    }


def render_recommendation(
    offers: List[ProductOffer],
    amount: Optional[int],
    term: Optional[int],
    org: Optional[str],
    age_category: Optional[str],
    is_young: bool = False,
) -> Dict[str, Any]:
    if not offers:
        return {
            "reply": (
                "По заданным критериям не удалось найти точного совпадения. "
                "Попробуйте скорректировать сумму или срок, либо выберите одну из популярных программ."
            ),
            "recommended_products": [],
            "actions": [],
            "quick_replies": ["Оборотные средства", "Экспресс-кредит", "Кредит до 1 млн"],
            "dialogue_state": "RECOMMENDATION",
        }

    best = offers[0]
    p = best.product

    rec_amount = best.amount
    rec_term = best.term_months

    lines = []
    lines.append(f"Рекомендую обратить внимание на программу **«{p.title}»**.")
    lines.append(f"• **Ставка:** от {p.rate:.1f}% годовых")
    lines.append(f"• **Ежемесячный платеж:** ~{format_rubles(best.payment)} / мес. (при сумме {format_rubles(rec_amount)} на {rec_term} мес.)")
    lines.append(f"• **Срок:** от {p.term[0]} до {p.term[1]} мес.")
    lines.append(f"• **Сумма:** от {format_rubles(p.amount[0])} до {format_rubles(p.amount[1])}")

    if is_young and p.young_max:
        lines.append(
            f"ℹ️ Для компаний младше 9 месяцев действует специальный лимит: **до {format_rubles(p.young_max)}**."
        )

    if best.why_matched:
        lines.append("\n**Почему подходит:**")
        for reason in best.why_matched:
            lines.append(f"✓ {reason}")

    if best.warnings:
        for w in best.warnings:
            lines.append(f"⚠️ {w}")

    if len(offers) > 1:
        other_titles = ", ".join(f"«{o.product.title}» ({o.product.rate:.1f}%)" for o in offers[1:])
        lines.append(f"\nТакже доступны альтернативы: {other_titles}.")

    lines.append("\nХотите применить эти параметры в калькуляторе или сразу перейти к оформлению?")

    actions: List[ActionCommand] = []

    actions.append(
        ActionCommand(
            type="apply_calculator",
            label=f"Выставить {format_rubles(rec_amount)} в калькуляторе",
            payload={
                "product_id": p.id,
                "amount": rec_amount,
                "term": rec_term,
                "org": org or (p.org_types[0] if p.org_types else "ooo"),
                "collateral": p.collateral if p.collateral != "any" else "without",
                "age": "lt9" if is_young else "gte9",
            },
        )
    )

    actions.append(
        ActionCommand(
            type="apply_wizard",
            label="Оформить заявку",
            payload={
                "product_id": p.id,
                "backend_id": p.backend_id,
                "amount": rec_amount,
                "term": rec_term,
                "org": org or (p.org_types[0] if p.org_types else "ooo"),
                "collateral": p.collateral if p.collateral != "any" else "without",
                "age": "lt9" if is_young else "gte9",
            },
        )
    )

    quick_replies = [
        "Какие документы нужны?",
        "Сравнить с другими",
        "Хочу сумму побольше",
        "Изменить срок",
        "Сколько рассматривают?",
        "Как подписать договор?",
    ]

    return {
        "reply": "\n".join(lines),
        "recommended_products": offers,
        "actions": actions,
        "quick_replies": quick_replies,
        "dialogue_state": "RECOMMENDATION",
    }


def render_comparison(
    p1: ProductSpec,
    p2: ProductSpec,
    offers: Optional[List[ProductOffer]] = None,
) -> Dict[str, Any]:
    lines = [
        f"**Сравнение программ «{p1.title}» и «{p2.title}»:**\n",
        f"**1. «{p1.title}»**",
        f"• Ставка: от {p1.rate:.1f}% годовых",
        f"• Сумма: {format_rubles(p1.amount[0])} — {format_rubles(p1.amount[1])}",
        f"• Срок: {p1.term[0]} — {p1.term[1]} мес.",
        f"• Залог: {'Требуется' if p1.collateral == 'with' else 'Без залога' if p1.collateral == 'without' else 'Любой'}",
        f"• Стаж бизнеса: {'От 9 месяцев' if p1.needs9 else 'От 0 месяцев (молодой бизнес)'}",
        f"• Особенность: {p1.desc}\n",
        f"**2. «{p2.title}»**",
        f"• Ставка: от {p2.rate:.1f}% годовых",
        f"• Сумма: {format_rubles(p2.amount[0])} — {format_rubles(p2.amount[1])}",
        f"• Срок: {p2.term[0]} — {p2.term[1]} мес.",
        f"• Залог: {'Требуется' if p2.collateral == 'with' else 'Без залога' if p2.collateral == 'without' else 'Любой'}",
        f"• Стаж бизнеса: {'От 9 месяцев' if p2.needs9 else 'От 0 месяцев (молодой бизнес)'}",
        f"• Особенность: {p2.desc}",
    ]

    actions = [
        ActionCommand(
            type="apply_calculator",
            label=f"Выбрать «{p1.title}»",
            payload={
                "product_id": p1.id,
                "amount": p1.amount[0] if p1.amount[0] > 1_000_000 else 1_000_000,
                "term": p1.term[0] if p1.term[0] > 12 else 12,
                "org": p1.org_types[0],
                "collateral": p1.collateral if p1.collateral != "any" else "without",
                "age": "gte9" if p1.needs9 else "lt9",
            },
        ),
        ActionCommand(
            type="apply_calculator",
            label=f"Выбрать «{p2.title}»",
            payload={
                "product_id": p2.id,
                "amount": p2.amount[0] if p2.amount[0] > 1_000_000 else 1_000_000,
                "term": p2.term[0] if p2.term[0] > 12 else 12,
                "org": p2.org_types[0],
                "collateral": p2.collateral if p2.collateral != "any" else "without",
                "age": "gte9" if p2.needs9 else "lt9",
            },
        ),
    ]

    return {
        "reply": "\n".join(lines),
        "recommended_products": offers or [],
        "actions": actions,
        "quick_replies": [f"Документы для «{p1.title}»", f"Документы для «{p2.title}»", "Оформить заявку"],
        "dialogue_state": "COMPARISON",
    }


def render_all_products() -> Dict[str, Any]:
    lines = [
        "В линейке Альфа-Банка доступно **14 кредитных продуктов** для малого и среднего бизнеса, сгруппированных по 5 ключевым направлениям:\n",
        "**1. Оборотное финансирование (Cash Flow):**",
        "• «Оборотные средства» (от 19.0%, до 30 млн ₽)",
        "• «Кредит для ООО без залога» (от 20.0%, до 30 млн ₽)",
        "• «Кредит для ИП без залога» (от 21.0%, до 15 млн ₽)",
        "• «Кредит для селлеров маркетплейсов» (от 18.5%, до 15 млн ₽)",
        "• «Экспресс-кредит для бизнеса» (от 20.5%, до 5 млн ₽)",
        "• «Кредит до 1 млн ₽» (от 22.0%, до 1 млн ₽)",
        "• «Овердрафт» (от 17.5%, до 15 млн ₽ на срок до 12 мес.)\n",
        "**2. Кредитные линии:**",
        "• «Возобновляемая кредитная линия (ВКЛ)» (от 17.0%, до 30 млн ₽)\n",
        "**3. Инвестиционные кредиты:**",
        "• «Кредит на оборудование» (от 16.5%, до 30 млн ₽)",
        "• «Автокредит для бизнеса» (от 15.5%, до 20 млн ₽)\n",
        "**4. Кредиты под залог недвижимости:**",
        "• «Кредит под залог недвижимости/оборудования» (от 15.0%, до 30 млн ₽)",
        "• «Ипотека для ООО» (от 14.5%, до 30 млн ₽)",
        "• «Ипотека для ИП» (от 14.5%, до 30 млн ₽)\n",
        "**5. Покупка бизнеса:**",
        "• «Покупка завода или готового бизнеса» (от 16.0%, до 30 млн ₽)\n",
        "Расскажите о вашей задаче, и я подберу подходящую программу за пару секунд!",
    ]

    return {
        "reply": "\n".join(lines),
        "recommended_products": [],
        "actions": [],
        "quick_replies": ["Оборотные средства", "Маркетплейсы", "Без залога до 1 года", "Автокредит"],
        "dialogue_state": "CATALOG_OVERVIEW",
    }


def render_docs(product_title: str, docs: List[str]) -> Dict[str, Any]:
    lines = [f"Для оформления продукта **«{product_title}»** потребуются следующие документы:\n"]
    for i, d in enumerate(docs, 1):
        lines.append(f"{i}. {d}")
    lines.append("\nВсе документы загружаются онлайн через личный кабинет, визит в отделение не требуется.")

    return {
        "reply": "\n".join(lines),
        "recommended_products": [],
        "actions": [
            ActionCommand(
                type="apply_wizard",
                label="Перейти к оформлению",
                payload={"product_title": product_title},
            )
        ],
        "quick_replies": ["Сколько рассматривают?", "Как подписать договор?", "Другой продукт"],
        "dialogue_state": "DOCS_CONSULTATION",
    }


def render_faq(faq_type: str) -> Dict[str, Any]:
    if faq_type == "faq_signing":
        reply = (
            "**Как подписать кредитный договор:**\n\n"
            "1. После одобрения заявки кредитный договор формируется в вашем личном кабинете.\n"
            "2. Вы проверяете финальные условия, график платежей и реквизиты счёта.\n"
            "3. Подписание происходит онлайн с помощью простой электронной подписи (СМС-код).\n"
            "4. Сразу после подписания средства зачисляются на ваш расчётный счёт без визита в банк."
        )
        quick_replies = ["Связаться с менеджером", "Подать заявку", "Необходимые документы"]
    elif faq_type == "faq_timeline":
        reply = (
            "**Сроки рассмотрения заявок в Альфа-Банке:**\n\n"
            "• **Экспресс-кредит и Кредит до 1 млн ₽:** от 2 минут в автоматическом скоринге.\n"
            "• **Селлеры маркетплейсов и Овердрафт:** от 15 минут до 2 часов.\n"
            "• **Оборотные средства и Кредитная линия:** в течение 1 рабочего дня.\n"
            "• **Залоговые программы, автокредит и ипотека:** от 1 до 3 рабочих дней (с учётом оценки залога).\n\n"
            "Статус рассмотрения отображается в режиме реального времени в личном кабинете."
        )
        quick_replies = ["Подать экспресс-заявку", "Статус заявки", "Какие документы нужны?"]
    elif faq_type == "faq_manager":
        reply = (
            "**Связь с персональным менеджером:**\n\n"
            "• Телефон горячей линии для бизнеса: **8 800 200-00-00** (бесплатно по РФ)\n"
            "• Чат в мобильном приложении и интернет-банке «Альфа-Бизнес»: доступен 24/7\n"
            "• Кредитный специалист закрепляется за вашей компанией сразу после подачи первой заявки."
        )
        quick_replies = ["Подать новую заявку", "Статус заявки", "Подобрать кредит"]
    else:
        reply = "Если у вас возникли вопросы по тарифам или оформлению, я готов помочь!"
        quick_replies = ["Подобрать кредит", "Все программы", "Статус заявки"]

    return {
        "reply": reply,
        "recommended_products": [],
        "actions": [],
        "quick_replies": quick_replies,
        "dialogue_state": "FAQ",
    }


def render_gratitude() -> Dict[str, Any]:
    reply = (
        "Рад был помочь! Если появятся вопросы по кредитным программам, "
        "расчётам платежей или документам — пишите, я всегда на связи."
    )
    return {
        "reply": reply,
        "recommended_products": [],
        "actions": [],
        "quick_replies": ["Подобрать другой кредит", "Все продукты", "Какие документы нужны?"],
        "dialogue_state": "IDLE",
    }


def render_status_help(
    status: Optional[str],
    missing_fields: Optional[List[str]] = None,
    missing_docs: Optional[List[str]] = None,
) -> Dict[str, Any]:
    status_upper = (status or "").upper()
    lines = []

    if status_upper == "INFO_REQUESTED":
        lines.append("⚠️ **Статус вашей заявки: Требуется уточнение данных (INFO_REQUESTED)**\n")
        lines.append("Кредитный аналитик запросил дополнительные сведения для продолжения рассмотрения.")
        if missing_docs:
            lines.append("\n**Недостающие документы:**")
            for d in missing_docs:
                lines.append(f"• {d}")
        if missing_fields:
            lines.append("\n**Необходимо заполнить поля в анкете:**")
            for f in missing_fields:
                lines.append(f"• {f}")
        lines.append("\nПожалуйста, прикрепите запрошенные документы в анкете заявки, чтобы мы могли вынести решение.")
    elif status_upper == "SUBMITTED":
        lines.append("⏳ **Статус заявки: Отправлена на рассмотрение (SUBMITTED)**\n")
        lines.append("Ваша анкета передана в скоринг. Обычно автоматическая проверка занимает от нескольких секунд до пары часов.")
    elif status_upper == "SCORING":
        lines.append("⚙️ **Статус заявки: Проверка данных и скоринг (SCORING)**\n")
        lines.append("Система анализирует кредитную историю, финансовую отчетность и бизнес-метрики.")
    elif status_upper == "APPROVED":
        lines.append("🎉 **Поздравляем! Заявка одобрена (APPROVED)**\n")
        lines.append("Вам сформирован кредитный договор. Вы можете подписать его электронной подписью в личном кабинете.")
    elif status_upper == "REJECTED":
        lines.append("❌ **Статус заявки: Отклонена (REJECTED)**\n")
        lines.append("К сожалению, по данной заявке вынесен отказ. Рекомендуем проверить кредитную историю или подать заявку на другой продукт с меньшей суммой.")
    else:
        lines.append("В личном кабинете отображаются все ваши текущие и завершенные заявки.")
        lines.append("Если у вас статус `INFO_REQUESTED`, просто загрузите запрошенные файлы в карточке заявки.")

    return {
        "reply": "\n".join(lines),
        "recommended_products": [],
        "actions": [],
        "quick_replies": ["Как подписать договор?", "Связаться с менеджером", "Подать новую заявку"],
        "dialogue_state": "STATUS_HELP",
    }


def render_increase_amount(
    product: Optional[ProductSpec] = None,
    current_amount: Optional[int] = None,
    is_young: bool = False,
) -> Dict[str, Any]:
    lines = ["**Увеличение суммы финансирования:**\n"]
    if is_young:
        lines.append(
            "ℹ️ Для компаний со стажем **до 9 месяцев** без залога действуют базовые лимиты: "
            "до **2 млн ₽** (Экспресс) и до **3 млн ₽** (Селлеры маркетплейсов)."
        )
        lines.append("\n**Как получить сумму больше прямо сейчас:**")
        lines.append("1. **Автокредит для бизнеса:** до **5 000 000 ₽** (в залог идёт приобретаемый транспорт).")
        lines.append("2. **Кредит под залог недвижимости или оборудования:** до **10 000 000 ₽** даже для молодых компаний.")
        lines.append("3. При стаже бизнеса **от 9 месяцев** лимит без залога возрастает до **30 000 000 ₽**.")
    else:
        lines.append("Для действующего бизнеса со стажем от 9 месяцев доступны повышенные лимиты:")
        lines.append("• **Без залога (ООО):** до **30 000 000 ₽** (Оборотные средства или ВКЛ).")
        lines.append("• **Без залога (ИП):** до **15 000 000 ₽**.")
        lines.append("• **С залогом недвижимости/оборудования:** до **30 000 000 ₽** по сниженной ставке от 15.0%.")

    lines.append("\nКакая сумма требуется вашему бизнесу?")

    quick_replies = ["5 млн ₽", "10 млн ₽", "15 млн ₽", "30 млн ₽", "Кредит под залог"]
    return {
        "reply": "\n".join(lines),
        "recommended_products": [],
        "actions": [],
        "quick_replies": quick_replies,
        "dialogue_state": "INCREASE_AMOUNT",
    }


def render_change_term(
    product: Optional[ProductSpec] = None,
    amount: Optional[int] = None,
) -> Dict[str, Any]:
    amt = amount or 1_000_000
    p = product or get_product_by_id("working-capital")
    min_t = p.term[0] if p else 3
    max_t = p.term[1] if p else 36
    rate = p.rate if p else 19.0

    lines = [
        f"**Расчет ежемесячного платежа для суммы {format_rubles(amt)}:**",
        f"Программа: **«{p.title if p else 'Оборотные средства'}»** (ставка от {rate:.1f}%)\n",
    ]

    terms_to_show = [t for t in [6, 12, 24, 36, 60, 120] if min_t <= t <= max_t]
    if not terms_to_show:
        terms_to_show = [min_t, max_t]

    actions: List[ActionCommand] = []
    quick_replies: List[str] = []

    for t in terms_to_show:
        if rate > 0:
            r_month = (rate / 100.0) / 12.0
            pmt = int(round(amt * (r_month * (1 + r_month) ** t) / ((1 + r_month) ** t - 1)))
        else:
            pmt = int(round(amt / max(t, 1)))
        lines.append(f"• **{format_term(t)}:** ~**{format_rubles(pmt)}** / мес.")

        if len(actions) < 2 and p:
            actions.append(
                ActionCommand(
                    type="apply_calculator",
                    label=f"Выбрать {format_term(t)}",
                    payload={
                        "product_id": p.id,
                        "amount": amt,
                        "term": t,
                        "org": p.org_types[0] if p.org_types else "ooo",
                        "collateral": p.collateral if p.collateral != "any" else "without",
                        "age": "gte9" if p.needs9 else "lt9",
                    },
                )
            )
        quick_replies.append(f"Срок {format_term(t)}")

    lines.append("\nВыберите комфортный срок в калькуляторе или нажмите на подходящий вариант ниже:")

    return {
        "reply": "\n".join(lines),
        "recommended_products": [],
        "actions": actions,
        "quick_replies": quick_replies[:4] + ["Сравнить с другими"],
        "dialogue_state": "CHANGE_TERM",
    }


def render_qa_answer(qa_item) -> Dict[str, Any]:
    lines = [f"**{qa_item.question}**\n", qa_item.answer]
    return {
        "reply": "\n".join(lines),
        "recommended_products": [],
        "actions": [],
        "quick_replies": qa_item.quick_replies,
        "dialogue_state": "QA_EXPERT",
    }


def render_reset_flow() -> Dict[str, Any]:
    reply = (
        "Параметры сброшены. Давайте начнем сначала!\n\n"
        "Расскажите, на какую цель и какую сумму вы ищете финансирование, "
        "или выберите одно из популярных направлений ниже:"
    )
    return {
        "reply": reply,
        "recommended_products": [],
        "actions": [],
        "quick_replies": [
            "3 млн на маркетплейсы",
            "Оборотные средства 1 млн",
            "Без залога на 1 год",
            "Автокредит для бизнеса",
        ],
        "dialogue_state": "GREETING",
    }


EASTER_EGG_REPLIES = {
    "welldone": "туймазы🍚",
    "artem": "（ ͜.人 ͜.）",
    "ruslan": "52",
    "sanya": "🦍",
    "max": "🍑",
}


def render_easter_egg(egg_key: str) -> Dict[str, Any]:
    text = EASTER_EGG_REPLIES.get(egg_key, "✨")
    return {
        "reply": text,
        "dialogue_state": "CHIT_CHAT",
        "recommended_products": [],
        "actions": [],
        "quick_replies": [
            "3 млн на маркетплейсы",
            "Оборотные средства 1 млн",
            "Что ты умеешь?",
            "Подобрать кредит",
        ],
    }


def render_chit_chat(intent: str) -> Dict[str, Any]:
    if intent == "chit_chat_how_are_you":
        reply = (
            "У меня всё отлично! Анализирую кредитные предложения, слежу за ставками "
            "и помогаю предпринимателям развивать бизнес.\n\n"
            "А как ваши дела? Какая сумма или задача сейчас актуальна?"
        )
        quick_replies = [
            "Оборотные средства 1 млн",
            "3 млн на маркетплейсы",
            "Что ты умеешь?",
            "Расскажи анекдот",
        ]
    elif intent == "chit_chat_who_are_you":
        reply = (
            "Я **Альфи** — ваш персональный виртуальный кредитный куратор и AI-помощник Альфа-Банка.\n\n"
            "Меня разработали специально для **Альфа-Хакатона 2026**. Я умею:\n"
            "• Мгновенно подбирать программы из 14 кредитных продуктов;\n"
            "• Рассчитывать платежи и выставлять параметры прямо в калькулятор;\n"
            "• Консультировать по более чем 50 финансовым и юридическим вопросам бизнеса."
        )
        quick_replies = [
            "Что ты умеешь?",
            "Подобрать кредит",
            "Сравнить с другими",
            "Дай совет",
        ]
    elif intent == "chit_chat_joke":
        reply = (
            "— Здравствуйте, я хочу взять кредит на открытие бизнеса.\n"
            "— А бизнес-план у вас есть?\n"
            "— Конечно! Беру кредит, открываю точку, получаю прибыль, отдаю кредит.\n"
            "— Гениально! А если прибыли не будет?\n"
            "— Ну тогда вы мне поможете оформить кредитные каникулы! 😄\n\n"
            "А если серьезно — у нас честные ставки и прозрачные условия без скрытых комиссий. Рассчитаем платеж?"
        )
        quick_replies = [
            "Без залога на 1 год",
            "Оборотные средства 1 млн",
            "Что ты умеешь?",
        ]
    elif intent == "chit_chat_advice":
        reply = (
            "💡 **Финансовый совет от Альфи для бизнеса:**\n\n"
            "1. **Не закрывайте овердрафтом долгосрочные проекты** — овердрафт идеален для кассовых разрывов до 60 дней, а для оборудования выбирайте целевой кредит на 3–5 лет.\n"
            "2. **Используйте транши кредитной линии (ВКЛ)** — проценты начисляются только на фактически используемую часть лимита.\n"
            "3. **Гасите досрочно** — в Альфа-Банке частичное и полное досрочное погашение осуществляется без комиссий и штрафов в любой день!"
        )
        quick_replies = [
            "Овердрафт для бизнеса",
            "Возобновляемая линия",
            "Подобрать кредит",
            "Сравнить с другими",
        ]
    elif intent == "chit_chat_farewell":
        reply = (
            "До свидания! Всегда рад помочь вашему делу расти. "
            "Если понадобятся расчеты или консультация — я всегда на связи в правом нижнем углу.\n\n"
            "Успехов вашему бизнесу и отличного дня! 🚀"
        )
        quick_replies = [
            "Подобрать кредит",
            "Что ты умеешь?",
            "Начать сначала",
        ]
    else:
        reply = "Всегда к вашим услугам! Чем могу помочь по кредитованию бизнеса?"
        quick_replies = ["Подобрать кредит", "Что ты умеешь?"]

    return {
        "reply": reply,
        "dialogue_state": "CHIT_CHAT",
        "recommended_products": [],
        "actions": [],
        "quick_replies": quick_replies,
    }


