"""
Natural Language Understanding (NLU) slot filler for Russian loan inquiries.
Extracts amount, term, legal form, collateral, business age, purpose, target products, and user intents.
"""
from __future__ import annotations
import re
from typing import Optional, Dict, Any, Tuple


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
        target_product_id: Optional[str] = None,
        intent: str = "general_inquiry",
    ):
        self.amount = amount
        self.term_months = term_months
        self.org = org
        self.collateral = collateral
        self.age_months = age_months
        self.age_category = age_category
        self.purpose = purpose
        self.target_product_id = target_product_id
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
            "target_product_id": self.target_product_id,
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
            target_product_id=other.target_product_id if other.target_product_id is not None else self.target_product_id,
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
        target_product_id = cls._extract_target_product(t)

        return ExtractedSlots(
            amount=amount,
            term_months=term,
            org=org,
            collateral=collateral,
            age_months=age_months,
            age_category=age_category,
            purpose=purpose,
            target_product_id=target_product_id,
            intent=intent,
        )

    @classmethod
    def _detect_intent(cls, text: str) -> str:
        # 0. Hidden Easter Egg Commands
        t_stripped = text.strip()
        if re.search(r"\b(welldone|well\s+done)\b", text) or t_stripped == "welldone":
            return "easter_egg_welldone"

        if re.search(r"\b(артем|артём|artem)\b", text):
            return "easter_egg_artem"

        if re.search(r"\b(руслан|ruslan)\b", text) or t_stripped == "52":
            return "easter_egg_ruslan"

        if re.search(r"\b(саня|саша|sanya|санек|санёк)\b", text):
            return "easter_egg_sanya"

        if re.search(r"\b(макс|максим|max)\b", text) and not any(w in text for w in ["сум", "лимит", "срок", "ставк", "процент", "платеж"]):
            return "easter_egg_max"

        # 0.1 Chit-Chat: How are you
        if re.search(r"\b(как\s+дела|как\s+поживаешь|как\s+настроение|как\s+жизнь|как\s+ты|че\s+как|как\s+оно)\b", text):
            return "chit_chat_how_are_you"

        # 0.2 Chit-Chat: Who are you / Creator
        if re.search(r"\b(кто\s+ты|кто\s+тебя\s+создал|как\s+тебя\s+зовут|откуда\s+ты|расскажи\s+о\s+себе|ты\s+человек\s+или|ты\s+бот)\b", text):
            return "chit_chat_who_are_you"

        # 0.3 Chit-Chat: Joke
        if re.search(r"\b(анекдот|шутк\w*|пошути|расскажи\s+анекдот|посмеши|знаешь\s+анекдот)\b", text):
            return "chit_chat_joke"

        # 0.4 Chit-Chat: Advice
        if re.search(r"\b(дай\s+совет|совет\s+дня|бизнес\s+совет|финансов\w*\s+совет|посоветуй\s+что-нибудь|что\s+посоветуешь)\b", text):
            return "chit_chat_advice"

        # 0.5 Chit-Chat: Farewell
        if re.search(r"\b(пока|до\s+свидания|прощай|до\s+встречи|хорошего\s+дня|спокойной\s+ночи|увидимся)\b", text):
            return "chit_chat_farewell"

        # 0.6 Bot Capabilities / How to use / Help
        if re.search(r"\b(что\s+ты\s+(умеешь|можешь)|чем\s+(ты\s+)?(можешь\s+)?помочь|как\s+(тобой\s+)?пользоваться|что\s+тебе\s+написать|что\s+(можно\s+)?(тебе\s+)?написать|какие\s+команды|возможности)\b", text):
            return "bot_capabilities"

        # 1. Greeting
        if re.search(r"\b(привет|здравствуй\w*|добрый\s+(день|вечер|утро)|хай|салют)\b", text):
            if not any(k in text for k in ["млн", "тыс", "руб", "кредит", "займ", "залог", "ип", "ооо"]):
                return "greeting"

        # 2. Gratitude / acknowledgment
        if re.search(r"\b(спасибо\w*|благодар\w*|отлично|супер|понятно|ясно|понял|договорились)\b", text):
            if not any(k in text for k in ["млн", "тыс", "руб", "кредит", "займ", "залог"]):
                return "gratitude"

        # 3. Increase Amount Command
        if re.search(r"\b(хочу\s+сумму\s+побольше|сумму\s+больше|нужно\s+больше|увеличить\s+сумму|лимит\s+выше|побольше\s+денег|больше\s+денег|мало\b)\b", text):
            return "increase_amount"

        # 4. Change Term Command
        if re.search(r"\b(изменить\s+срок|другой\s+срок|срок\s+больше|срок\s+меньше|подольше|покороче|выбрать\s+срок)\b", text):
            return "change_term"

        # 5. Reset / Other Product Command
        if re.search(r"\b(другой\s+продукт|подать\s+новую\s+заявку|начать\s+сначала|сбросить|выбрать\s+другое|в\s+начало)\b", text):
            return "reset_flow"

        # 6. FAQ: Signing
        if re.search(r"\b(подписа\w*.*договор|электронн\w*\s+подпис\w*|смс.*код|как\s+подписать)\b", text):
            return "faq_signing"

        # 7. FAQ: Timeline / consideration speed
        if re.search(r"\b(скольк\w*\s+(по\s+времени\s+)?рассматрива\w*|срок\w*\s+рассмотрен\w*|как\s+долго\s+рассматрива\w*|время\s+рассмотрен\w*|когда\s+деньги|когда\s+придут)\b", text):
            return "faq_timeline"

        # 8. FAQ: Manager contact
        if re.search(r"\b(связа\w*.*менеджер\w*|позвонить|телефон\s+поддержк\w*|номер\s+банк\w*|горяч\w*\s+лини\w*|оператор\w*)\b", text):
            return "faq_manager"

        # 9. Documents
        if re.search(r"\b(какие\s+документ\w*|документ\w*|доки|справк\w*|выписк\w*|паспорт\w*|что\s+нужно\s+(собрать|предоставить|принести))\b", text):
            return "docs_consultation"

        # 10. Status
        if re.search(r"\b(статус\w*|заявк\w*|info_requested|рассмотрен\w*|одобрен\w*|на\s+доработк\w*)\b", text) or "с заявкой" in text or "по заявке" in text:
            return "status_inquiry"

        # 11. Comparison
        if re.search(r"\b(сравн\w*|чем\s+отлича\w*|разниц\w*|что\s+лучше|в\s+чем\s+отличи\w*|покажи\s+другие)\b", text):
            return "comparison"

        # 12. All products overview
        if re.search(r"\b(все\s+кредит\w*|все\s+продукт\w*|какие\s+(бывают\s+)?(кредит\w*|продукт\w*)|список\s+(кредит\w*|программ|продукт\w*)|каталог)\b", text):
            return "all_products"

        # 13. General inquiry / collecting slots
        if re.search(r"\b(помоги\w*\s+выбрать|посоветуй\w*|какой\s+кредит|хочу\s+взять\s+кредит|нужен\s+кредит|подбер\w*)\b", text):
            if not any(k in text for k in ["млн", "тыс", "руб", "маркетплейс", "авто", "оборудован", "ипотек", "завод", "овердрафт", "вкл", "линия", "залог"]):
                return "collect_slots"
            return "recommendation"

        if re.search(r"\b(подбер\w*|рассчитай\w*|оформить)\b", text):
            return "recommendation"

        return "loan_inquiry"

    @classmethod
    def _extract_target_product(cls, text: str) -> Optional[str]:
        if "овердрафт" in text:
            return "overdraft"
        if "вкл" in text or "кредитная линия" in text or "кредитную линию" in text:
            return "credit-line"
        if "экспресс" in text:
            return "express"
        if "до 1 млн" in text or "до миллиона" in text:
            return "small"
        if "селлер" in text or "маркетплейс" in text or "wildberries" in text or "ozon" in text:
            return "seller"
        if "автокредит" in text or "авто" in text or "машин" in text:
            return "auto"
        if "оборудован" in text or "станок" in text:
            return "equipment"
        if "ипотек" in text:
            if "ип" in text and "ооо" not in text:
                return "mortgage-ip"
            return "mortgage-ooo"
        if "залог недвижимости" in text or "под залог имущества" in text:
            return "secured-property"
        if "завод" in text or "готов" in text and "бизнес" in text:
            return "ready-business"
        if "для ооо без залога" in text:
            return "ooo-unsecured"
        if "для ип без залога" in text:
            return "ip-unsecured"
        if "оборотные средства" in text or "оборотк" in text:
            return "working-capital"
        return None

    @classmethod
    def _extract_amount(cls, text: str) -> Optional[int]:
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

        # Strip business age phrases so they are never mistakenly parsed as loan terms!
        clean_for_term = re.sub(
            r"(?:бизнесу|работаем|существуем|открылись|стаж|компании)\s*(?:уже\s*)?\d+(?:\.\d+)?\s*(?:мес\w*|год\w*|лет)\b",
            " ",
            clean,
        )
        clean_for_term = re.sub(r"\b\d+(?:\.\d+)?\s*(?:мес\w*|год\w*|лет)\s+назад\b", " ", clean_for_term)

        # "на полтора года" / "полтора года"
        if re.search(r"\bполтора\s+года\b", clean_for_term):
            return 18

        # "на полгода" / "полгода"
        if re.search(r"\bполгода\b", clean_for_term):
            return 6

        # "на 1.5 года", "на 2 года", "на 5 лет"
        year_match = re.search(r"(?:на\s+)?(\d+(?:\.\d+)?)\s*(?:год|года|лет|г\.)\b", clean_for_term)
        if year_match:
            try:
                years = float(year_match.group(1))
                return int(round(years * 12))
            except ValueError:
                pass

        # "на 6 месяцев", "на 18 мес"
        month_match = re.search(r"(?:на\s+)?(\d+)\s*(?:мес|месяц|месяца|месяцев|м\.)\b", clean_for_term)
        if month_match:
            try:
                return int(month_match.group(1))
            except ValueError:
                pass

        if re.search(r"\b(на\s+)?(один\s+)?месяц\b", clean_for_term):
            return 1

        if re.search(r"\b(на\s+)?(один\s+)?год\b", clean_for_term):
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
    def _extract_business_age(cls, text: str) -> Tuple[Optional[int], Optional[str]]:
        clean = text.replace(",", ".")

        # Match "бизнесу X месяцев", "работаем X мес", "открылись X месяцев назад"
        age_m = re.search(r"(?:бизнесу|работаем|существуем|открылись|стаж|компании)\s*(?:уже\s*)?(\d+)\s*(?:мес|месяц|месяца|месяцев)\b", clean)
        if age_m:
            months = int(age_m.group(1))
            return months, "lt9" if months < 9 else "gte9"

        # Match "X месяцев назад"
        ago_m = re.search(r"\b(\d+)\s*(?:мес|месяц|месяца|месяцев)\s+назад\b", clean)
        if ago_m:
            months = int(ago_m.group(1))
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
        if re.search(r"\b(маркетплейс\w*|вайлдберриз|wildberries|вб|wb|озон\w*|ozon|яндекс\s*маркет|селлер\w*)\b", text):
            return "marketplace"

        if re.search(r"\b(овердрафт\w*|кассов\w*\s+разрыв|срочно\s+деньги|зарплат\w*|налоги|закрыть\s+дыру|перехватить)\b", text):
            return "cashflow_gap"

        if re.search(r"\b(вкл|кредитн\w*\s+лини\w*|транш\w*|возобновляем\w*)\b", text):
            return "credit_line"

        if re.search(r"\b(авто|автокредит\w*|автомобил\w*|машин\w*|транспорт\w*|грузов\w*|грузовик\w*|газел\w*|тягач\w*|фургон\w*)\b", text):
            return "vehicle"

        if re.search(r"\b(оборудовани\w*|станок\w*|станки|линия|техник\w*|аппарат\w*|печь|погрузчик\w*)\b", text):
            return "equipment"

        if re.search(r"\b(ипотек\w*|недвижим\w*|помещени\w*|офис\w*|склад\w*|здание|цех|коммерческ\w*\s+недвижим\w*)\b", text):
            return "real_estate"

        if re.search(r"\b(завод\w*|покупк\w*.*(бизнес\w*|предприяти\w*|доли)|готов\w*\s+бизнес\w*|купить\s+бизнес\w*)\b", text):
            return "business_purchase"

        if re.search(r"\b(экспресс\w*|быстр\w*\s+кредит|срочн\w*\s+кредит)\b", text):
            return "express"

        if re.search(r"\b(до\s+1\s+млн|до\s+миллиона|микро\w*)\b", text):
            return "small_loan"

        if re.search(r"\b(оборотк\w*|оборотн\w*|закупк\w*|товар\w*|материал\w*|пополнить\s+оборот\w*|текущ\w*\s+расход\w*)\b", text):
            return "working_capital"

        return None
