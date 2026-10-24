"""
Templates and formatting for the Credit Assistant Curator.
Businesslike, friendly, helpful tone of voice (Alfa-Bank style).
"""
from __future__ import annotations
from typing import List, Optional, Dict, Any
from assistant.domain.models import ProductOffer, ActionCommand


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
            "Здравствуйте! Я ваш куратор по кредитным продуктам.\n\n"
            "Помогу сориентироваться в условиях, проверить статус заявки "
            "или подсказать, какие документы нужны банку."
        )
        quick_replies = ["Статус заявки", "Необходимые документы", "Подобрать новый кредит"]
    else:
        text = (
            "Здравствуйте! Я онлайн-куратор кредитных продуктов для бизнеса.\n\n"
            "Помогу быстро подобрать оптимальную программу из 14 продуктов банка: "
            "на оборотные средства, маркетплейсы, авто или недвижимость. "
            "Расскажите, какая сумма и срок вам нужны?"
        )
        quick_replies = [
            "3 млн на маркетплейсы",
            "Оборотные средства 1 млн",
            "Без залога на 1 год",
            "Автокредит для бизнеса",
        ]

    return {
        "reply": text,
        "recommended_products": [],
        "actions": [],
        "quick_replies": quick_replies,
        "dialogue_state": "GREETING",
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

    lines = []
    lines.append(f"Рекомендую обратить внимание на программу **«{p.title}»**.")
    lines.append(f"• **Ставка:** от {p.rate:.1f}% годовых")
    lines.append(f"• **Ежемесячный платеж:** ~{format_rubles(best.payment)} / мес.")
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
    rec_amount = amount or p.amount[0]
    rec_term = term or p.term[0]
    if p.young_max and is_young and rec_amount > p.young_max:
        rec_amount = p.young_max

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
    ]

    return {
        "reply": "\n".join(lines),
        "recommended_products": offers,
        "actions": actions,
        "quick_replies": quick_replies,
        "dialogue_state": "RECOMMENDATION",
    }


def render_docs(product_title: str, docs: List[str]) -> Dict[str, Any]:
    lines = [f"Для оформления продукта **«{product_title}»** потребуются следующие документы:\n"]
    for i, d in enumerate(docs, 1):
        lines.append(f"{i}. {d}")
    lines.append("\nВсе документы загружаются онлайн через личный кабинет, визит в отделение не требуется.")

    return {
        "reply": "\n".join(lines),
        "recommended_products": [],
        "actions": [],
        "quick_replies": ["Перейти к заявке", "Другой продукт", "Сколько рассматривают?"],
        "dialogue_state": "DOCS_CONSULTATION",
    }


def render_status_help(status: Optional[str], missing_fields: Optional[List[str]] = None, missing_docs: Optional[List[str]] = None) -> Dict[str, Any]:
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
