import unittest
from assistant.engine.slot_filler import SlotFiller


class TestSlotFiller(unittest.TestCase):
    def test_amount_extraction(self):
        cases = [
            ("Нужно 3 млн на закупку", 3_000_000),
            ("Хотим взять 500 тыс рублей", 500_000),
            ("Требуется 1.5 миллиона", 1_500_000),
            ("Сумма 1,5 млн", 1_500_000),
            ("Около 300к на рекламу", 300_000),
            ("Запрашиваем 800 000 руб", 800_000),
            ("До миллиона рублей", 1_000_000),
            ("15 млн на развитие", 15_000_000),
        ]
        for text, expected in cases:
            slots = SlotFiller.extract(text)
            self.assertEqual(slots.amount, expected, f"Failed on text: {text}")

    def test_term_extraction(self):
        cases = [
            ("на 2 года", 24),
            ("на 6 месяцев", 6),
            ("на полгода", 6),
            ("на полтора года", 18),
            ("на 18 месяцев", 18),
            ("на 5 лет", 60),
            ("на 1 год", 12),
            ("на месяц", 1),
        ]
        for text, expected in cases:
            slots = SlotFiller.extract(text)
            self.assertEqual(slots.term_months, expected, f"Failed on text: {text}")

    def test_term_does_not_confuse_with_business_age(self):
        slots1 = SlotFiller.extract("хотим взять автокредит 4 млн, ооо, компании 7 месяцев")
        self.assertIsNone(slots1.term_months)
        self.assertEqual(slots1.age_months, 7)
        self.assertEqual(slots1.age_category, "lt9")

        slots2 = SlotFiller.extract("кредит 10 млн на 2 года, работаем 5 месяцев")
        self.assertEqual(slots2.term_months, 24)
        self.assertEqual(slots2.age_months, 5)

        slots3 = SlotFiller.extract("нужно 3 млн на закупку, открылись 4 месяца назад")
        self.assertIsNone(slots3.term_months)
        self.assertEqual(slots3.age_months, 4)

    def test_org_extraction(self):
        self.assertEqual(SlotFiller.extract("мы ООО Ромашка").org, "ooo")
        self.assertEqual(SlotFiller.extract("я юрлицо").org, "ooo")
        self.assertEqual(SlotFiller.extract("я ИП Иванов").org, "ip")
        self.assertEqual(SlotFiller.extract("индивидуальный предприниматель").org, "ip")

    def test_collateral_extraction(self):
        self.assertEqual(SlotFiller.extract("кредит без залога").collateral, "without")
        self.assertEqual(SlotFiller.extract("нет залога").collateral, "without")
        self.assertEqual(SlotFiller.extract("под залог недвижимости").collateral, "with")
        self.assertEqual(SlotFiller.extract("есть автомобиль для залога").collateral, "with")

    def test_business_age_extraction(self):
        slots1 = SlotFiller.extract("бизнесу 5 месяцев")
        self.assertEqual(slots1.age_months, 5)
        self.assertEqual(slots1.age_category, "lt9")

        slots2 = SlotFiller.extract("работаем 2 года")
        self.assertEqual(slots2.age_months, 24)
        self.assertEqual(slots2.age_category, "gte9")

        slots3 = SlotFiller.extract("у нас молодой бизнес")
        self.assertEqual(slots3.age_category, "lt9")

    def test_purpose_and_product_extraction(self):
        self.assertEqual(SlotFiller.extract("закупка под маркетплейс wildberries").purpose, "marketplace")
        self.assertEqual(SlotFiller.extract("кассовый разрыв, нечем платить зарплату").purpose, "cashflow_gap")
        self.assertEqual(SlotFiller.extract("покупка грузового автомобиля").purpose, "vehicle")
        self.assertEqual(SlotFiller.extract("купить производственный станок").purpose, "equipment")
        self.assertEqual(SlotFiller.extract("коммерческая ипотека под склад").purpose, "real_estate")
        self.assertEqual(SlotFiller.extract("покупка готового завода").purpose, "business_purchase")

        # Explicit product names
        self.assertEqual(SlotFiller.extract("хочу автокредит").target_product_id, "auto")
        self.assertEqual(SlotFiller.extract("нужен овердрафт").target_product_id, "overdraft")
        self.assertEqual(SlotFiller.extract("кредитная линия вкл").target_product_id, "credit-line")

    def test_intents_detection(self):
        self.assertEqual(SlotFiller.extract("Чем отличается овердрафт от кредитной линии?").intent, "comparison")
        self.assertEqual(SlotFiller.extract("Как подписать договор?").intent, "faq_signing")
        self.assertEqual(SlotFiller.extract("Сколько рассматривают заявку?").intent, "faq_timeline")
        self.assertEqual(SlotFiller.extract("Связаться с менеджером").intent, "faq_manager")
        self.assertEqual(SlotFiller.extract("Спасибо за помощь!").intent, "gratitude")
        self.assertEqual(SlotFiller.extract("Помогите выбрать кредит").intent, "collect_slots")
        self.assertEqual(SlotFiller.extract("Какие кредитные продукты у вас есть?").intent, "all_products")

    def test_easter_eggs_intents(self):
        self.assertEqual(SlotFiller.extract("welldone").intent, "easter_egg_welldone")
        self.assertEqual(SlotFiller.extract("WellDone").intent, "easter_egg_welldone")
        self.assertEqual(SlotFiller.extract("well done").intent, "easter_egg_welldone")
        self.assertEqual(SlotFiller.extract("Артем").intent, "easter_egg_artem")
        self.assertEqual(SlotFiller.extract("артём").intent, "easter_egg_artem")
        self.assertEqual(SlotFiller.extract("Руслан").intent, "easter_egg_ruslan")
        self.assertEqual(SlotFiller.extract("руслан").intent, "easter_egg_ruslan")
        self.assertEqual(SlotFiller.extract("52").intent, "easter_egg_ruslan")
        self.assertEqual(SlotFiller.extract("Саня").intent, "easter_egg_sanya")
        self.assertEqual(SlotFiller.extract("саня").intent, "easter_egg_sanya")
        self.assertEqual(SlotFiller.extract("Макс").intent, "easter_egg_max")
        self.assertEqual(SlotFiller.extract("макс").intent, "easter_egg_max")

    def test_chit_chat_intents(self):
        self.assertEqual(SlotFiller.extract("Как дела?").intent, "chit_chat_how_are_you")
        self.assertEqual(SlotFiller.extract("Как поживаешь?").intent, "chit_chat_how_are_you")
        self.assertEqual(SlotFiller.extract("Кто ты?").intent, "chit_chat_who_are_you")
        self.assertEqual(SlotFiller.extract("Кто тебя создал?").intent, "chit_chat_who_are_you")
        self.assertEqual(SlotFiller.extract("Расскажи анекдот").intent, "chit_chat_joke")
        self.assertEqual(SlotFiller.extract("Пошути").intent, "chit_chat_joke")
        self.assertEqual(SlotFiller.extract("Дай совет для бизнеса").intent, "chit_chat_advice")
        self.assertEqual(SlotFiller.extract("Пока!").intent, "chit_chat_farewell")
        self.assertEqual(SlotFiller.extract("До свидания").intent, "chit_chat_farewell")


if __name__ == "__main__":
    unittest.main()
