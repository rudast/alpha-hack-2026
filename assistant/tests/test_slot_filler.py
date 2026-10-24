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

    def test_purpose_extraction(self):
        self.assertEqual(SlotFiller.extract("закупка под маркетплейс wildberries").purpose, "marketplace")
        self.assertEqual(SlotFiller.extract("кассовый разрыв, нечем платить зарплату").purpose, "cashflow_gap")
        self.assertEqual(SlotFiller.extract("покупка грузового автомобиля").purpose, "vehicle")
        self.assertEqual(SlotFiller.extract("купить производственный станок").purpose, "equipment")
        self.assertEqual(SlotFiller.extract("коммерческая ипотека под склад").purpose, "real_estate")
        self.assertEqual(SlotFiller.extract("покупка готового завода").purpose, "business_purchase")


if __name__ == "__main__":
    unittest.main()
