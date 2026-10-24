"""
Natural Language Understanding (NLU) slot filler for Russian loan inquiries.
Extracts amount, term, legal form, collateral, business age, purpose, and user intents.
"""
from __future__ import annotations
import re
from typing import Optional, Dict, Any


class ExtractedSlots:
    def __init__(
        self,
        amount: Optional[int] = None,
        term_months: Optional[int] = None,
        org: Optional[str] = None,
        collateral: Optional[str] = None,
        age_months: Optional[int] = None,
        age_category: Optional[str] = None,  # 'gte9' or 'lt9'
        purpose: Optional[str] = None,
        intent: str = "general_inquiry",
    ):
        self.amount = amount
        self.term_months = term_months
        self.org = org
        self.collateral = collateral
        self.age_months = age_months
        self.age_category = age_category
        self.purpose = purpose
        self.intent = intent

    def to_dict(self) -> Dict[str, Any]:
        return {
            "amount": self.amount,
            "term_months": self.term_months,
            "org": self.org,
            "collateral": self.collateral,
            "age_months": self.age_months,
            "age_category": self.age_category,
            "purpose": self.purpose,
            "intent": self.intent,
        }

    def merge_with(self, other: ExtractedSlots) -> ExtractedSlots:
        return ExtractedSlots(
            amount=other.amount if other.amount is not None else self.amount,
            term_months=other.term_months if other.term_months is not None else self.term_months,
            org=other.org if other.org is not None else self.org,
            collateral=other.collateral if other.collateral is not None else self.collateral,
            age_months=other.age_months if other.age_months is not None else self.age_months,
            age_category=other.age_category if other.age_category is not None else self.age_category,
            purpose=other.purpose if other.purpose is not None else self.purpose,
            intent=other.intent if other.intent != "general_inquiry" else self.intent,
        )


class SlotFiller:
    """Parser of free-form Russian text into structured credit parameter slots."""

    @classmethod
    def extract(cls, text: str) -> ExtractedSlots:
        if not text:
            return ExtractedSlots()

        t = text.lower().replace("ё", "е")

        intent = cls._detect_intent(t)
        amount = cls._extract_amount(t)
        term = cls._extract_term(t)
        org = cls._extract_org(t)
        collateral = cls._extract_collateral(t)
        age_months, age_category = cls._extract_business_age(t)
        purpose = cls._extract_purpose(t)

        return ExtractedSlots(
            amount=amount,
            term_months=term,
            org=org,
            collateral=collateral,
            age_months=age_months,
            age_category=age_category,
            purpose=purpose,
            intent=intent,
        )

    @classmethod
    def _detect_intent(cls, text: str) -> str:
        if re.search(r"\b(привет|здравствуй\w*|добрый\s+(день|вечер|утро)|хай|салют)\b", text):
            # Check if there are other slots in the message
            if not any(k in text for k in ["млн", "тыс", "руб", "кредит", "займ", "залог", "ип", "ооо"]):
                return "greeting"

        if re.search(r"\b(документ\w*|доки|справк\w*|выписк\w*|паспорт\w*|что\s+нужно\s+(собрать|предоставить|принести))\b", text):
            return "docs_consultation"

        if re.search(r"\b(статус\w*|заявк\w*|info_requested|рассмотрен\w*|одобрен\w*|на\s+доработк\w*)\b", text) or "с заявкой" in text or "по заявке" in text:
            return "status_inquiry"

        if re.search(r"\b(сравн\w*|чем\s+отлича\w*|разниц\w*|что\s+лучше|посоветуй)\b", text):
            return "comparison"

        if re.search(r"\b(подбер\w*|рассчитай\w*|помоги\s+выбрать|нужен\s+кредит|хочу\s+взять|ищу\s+кредит|оформить)\b", text):
            return "recommendation"

        return "loan_inquiry"

    @classmethod
    def _extract_amount(cls, text: str) -> Optional[int]:
        # Clean non-breaking spaces
        clean = text.replace("\u00a0", " ").replace(",", ".")

        # Case: "1.5 млн", "3 миллиона", "15 млн"
        mln_match = re.search(r"(\d+(?:\.\d+)?)\s*(?:млн|миллион\w*|лям\w*)", clean)
        if mln_match:
            try:
                val = float(mln_match.group(1))
                return int(val * 1_000_000)
            except ValueError:
                pass

        # Case: "500 тыс", "300к", "500 тысяч"
        th_match = re.search(r"(\d+(?:\.\d+)?)\s*(?:тыс\w*|к|k)\b", clean)
        if th_match:
            try:
                val = float(th_match.group(1))
                return int(val * 1_000)
            except ValueError:
                pass

        # Case: full digits with spaces like "1 500 000" or "500000"
        digits_spaced = re.findall(r"\b\d{1,3}(?:\s\d{3})+\b", clean)
        if digits_spaced:
            # take the largest reasonable loan number
            for d in digits_spaced:
                num = int(d.replace(" ", ""))
                if 50_000 <= num <= 50_000_000:
                    return num

        # Plain digits with optional currency: 500000 руб
        plain_match = re.findall(r"\b(\d{5,8})\b", clean)
        if plain_match:
            for p in plain_match:
                num = int(p)
                if 50_000 <= num <= 50_000_000:
                    return num

        # Textual amounts: "до миллиона" / "миллион"
        if re.search(r"\bмиллион\w*\b", clean) and not mln_match:
            return 1_000_000

        return None

    @classmethod
    def _extract_term(cls, text: str) -> Optional[int]:
        clean = text.replace(",", ".")

        # "на полтора года" / "полтора года"
        if re.search(r"\bполтора\s+года\b", clean):
            return 18

        # "на полгода" / "полгода"
        if re.search(r"\bполгода\b", clean):
            return 6

        # "на 1.5 года", "на 2 года", "на 5 лет"
        year_match = re.search(r"(?:на\s+)?(\d+(?:\.\d+)?)\s*(?:год|года|лет|г\.)\b", clean)
        if year_match:
            try:
                years = float(year_match.group(1))
                return int(round(years * 12))
            except ValueError:
                pass

        # "на 6 месяцев", "на 18 мес", "на месяц"
        month_match = re.search(r"(?:на\s+)?(\d+)\s*(?:мес|месяц|месяца|месяцев|м\.)\b", clean)
        if month_match:
            try:
                return int(month_match.group(1))
            except ValueError:
                pass

        if re.search(r"\b(на\s+)?(один\s+)?месяц\b", clean):
            return 1

        if re.search(r"\b(на\s+)?(один\s+)?год\b", clean):
            return 12

        return None

    @classmethod
    def _extract_org(cls, text: str) -> Optional[str]:
        # IP keywords
        if re.search(r"\b(ип|индивидуальн\w*\s+предпринимател\w*|самозанят\w*)\b", text):
            return "ip"
        # OOO keywords
        if re.search(r"\b(ооо|юрлиц\w*|юридическ\w*\s+лиц\w*|организаци\w*|компани\w*)\b", text):
            return "ooo"
        return None

    @classmethod
    def _extract_collateral(cls, text: str) -> Optional[str]:
        # Explicit without collateral
        if re.search(r"\b(без\s+залога|без\s+обеспечени\w*|нет\s+залога|залог\s+не\s+хоч\w*|не\s+под\s+залог)\b", text):
            return "without"

        # Explicit with collateral
        if re.search(r"\b(с\s+залогом|под\s+залог|есть\s+залог|есть\s+недвижим\w*|есть\s+авто\w*|есть\s+квартир\w*|есть\s+помещени\w*|есть\s+машин\w*|под\s+обеспечени\w*)\b", text):
            return "with"

        return None

    @classmethod
    def _extract_business_age(cls, text: str) -> tuple[Optional[int], Optional[str]]:
        clean = text.replace(",", ".")

        # Match "бизнесу X месяцев", "работаем X мес", "открылись X месяцев назад"
        age_m = re.search(r"(?:бизнесу|работаем|существуем|открылись|стаж|компании)\s*(?:уже\s*)?(\d+)\s*(?:мес|месяц|месяца|месяцев)\b", clean)
        if age_m:
            months = int(age_m.group(1))
            return months, "lt9" if months < 9 else "gte9"

        # Match years: "бизнесу 2 года"
        age_y = re.search(r"(?:бизнесу|работаем|существуем|компании)\s*(?:уже\s*)?(\d+(?:\.\d+)?)\s*(?:год|года|лет)\b", clean)
        if age_y:
            months = int(float(age_y.group(1)) * 12)
            return months, "lt9" if months < 9 else "gte9"

        # Generic phrases: "молодой бизнес", "недавно открылись", "меньше 9 месяцев", "до 9 месяцев"
        if re.search(r"\b(молод\w*\s+бизнес|недавно\s+открыл\w*|нович\w*|меньше\s+9\s+мес\w*|до\s+9\s+мес\w*|младше\s+9\s+мес\w*|меньше\s+года)\b", clean):
            return 5, "lt9"

        # Generic phrases: "от 9 месяцев", "больше года", "давно работаем", "зрелый бизнес"
        if re.search(r"\b(от\s+9\s+мес\w*|более\s+9\s+мес\w*|старше\s+9\s+мес\w*|больше\s+года|давно\s+работаем|зрел\w*\s+бизнес)\b", clean):
            return 24, "gte9"

        return None, None

    @classmethod
    def _extract_purpose(cls, text: str) -> Optional[str]:
        if re.search(r"\b(маркетплейс|вайлдберриз|wildberries|вб|wb|озон|ozon|яндекс\s*маркет)\b", text):
            return "marketplace"

        if re.search(r"\b(кассов\w*\s+разрыв|срочно\s+деньги|зарплат\w*|налоги|закрыть\s+дыру|перехватить)\b", text):
            return "cashflow_gap"

        if re.search(r"\b(оборотк\w*|оборотн\w*|закупк\w*|товар\w*|материал\w*|пополнить\s+оборот\w*)\b", text):
            return "working_capital"

        if re.search(r"\b(авто|автомобил\w*|машин\w*|транспорт\w*|грузов\w*|грузовик\w*|газел\w*|тягач\w*|фургон\w*)\b", text):
            return "vehicle"

        if re.search(r"\b(оборудовани\w*|станок\w*|станки|линия|техник\w*|аппарат\w*|печь|погрузчик\w*)\b", text):
            return "equipment"

        if re.search(r"\b(ипотек\w*|недвижим\w*|помещени\w*|офис\w*|склад\w*|здание|цех|коммерческ\w*\s+недвижим\w*)\b", text):
            return "real_estate"

        if re.search(r"\b(завод\w*|покупк\w*.*(бизнес\w*|предприяти\w*|доли)|готов\w*\s+бизнес\w*|купить\s+бизнес\w*)\b", text):
            return "business_purchase"

        return None
