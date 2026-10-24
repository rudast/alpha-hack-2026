"""
Credit product matching engine.
Filters, scores, and ranks products from the canonical catalog based on user criteria.
Computes annuity payments and generates transparent explanations.
"""
from __future__ import annotations
import math
from typing import List, Optional, Tuple
from assistant.domain.models import ProductSpec, ProductOffer
from assistant.domain.catalog import get_all_products, get_product_by_id


def compute_annuity(amount: int, annual_rate: float, term_months: int) -> Tuple[int, int]:
    """
    Computes monthly annuity payment and total overpayment.
    Formula: PMT = P * (r * (1 + r)^n) / ((1 + r)^n - 1)
    where r = annual_rate / 100 / 12, n = term_months.
    """
    if term_months <= 0 or amount <= 0:
        return 0, 0

    monthly_rate = (annual_rate / 100.0) / 12.0
    if monthly_rate == 0:
        pmt = amount / term_months
    else:
        factor = math.pow(1.0 + monthly_rate, term_months)
        pmt = amount * (monthly_rate * factor) / (factor - 1.0)

    payment = int(round(pmt))
    overpayment = int(round(payment * term_months - amount))
    return payment, max(0, overpayment)


class ProductMatcher:
    """Matches user requests against the 14 bank credit products."""

    @classmethod
    def max_amount_for(cls, product: ProductSpec, age_months: Optional[int], age_category: Optional[str]) -> int:
        is_young = (age_months is not None and age_months < 9) or (age_category == "lt9")
        if is_young and product.young_max:
            return min(product.amount[1], product.young_max)
        return product.amount[1]

    @classmethod
    def is_strictly_available(
        cls,
        product: ProductSpec,
        org: Optional[str],
        collateral: Optional[str],
        age_months: Optional[int],
        age_category: Optional[str],
        amount: Optional[int],
        term_months: Optional[int],
    ) -> bool:
        # Org type check
        if org and org.lower() not in product.org_types:
            return False

        # Collateral check
        if collateral and collateral != "any" and product.collateral != "any":
            if product.collateral != collateral:
                return False

        # Business age check
        is_young = (age_months is not None and age_months < 9) or (age_category == "lt9")
        if is_young and product.needs9:
            return False

        # Amount check
        if amount is not None and amount > 0:
            max_limit = cls.max_amount_for(product, age_months, age_category)
            if amount < product.amount[0] or amount > max_limit:
                return False

        # Term check
        if term_months is not None and term_months > 0:
            if term_months < product.term[0] or term_months > product.term[1]:
                return False

        return True

    @classmethod
    def match(
        cls,
        org: Optional[str] = None,
        collateral: Optional[str] = None,
        age_months: Optional[int] = None,
        age_category: Optional[str] = None,
        amount: Optional[int] = None,
        term_months: Optional[int] = None,
        purpose: Optional[str] = None,
        target_product_id: Optional[str] = None,
        limit: int = 3,
    ) -> List[ProductOffer]:
        all_products = get_all_products()

        # Defaults for financial calculation based on purpose
        calc_amount = amount if amount and amount > 0 else 1_000_000
        if term_months and term_months > 0:
            calc_term = term_months
        elif purpose == "marketplace":
            calc_term = 18
        elif purpose in ("vehicle", "equipment"):
            calc_term = 36
        elif purpose in ("real_estate", "business_purchase"):
            calc_term = 60
        else:
            calc_term = 12

        is_young = (age_months is not None and age_months < 9) or (age_category == "lt9")

        # Check if user specifically requested a restricted product
        requested_product_warning: Optional[str] = None
        if target_product_id:
            tp = get_product_by_id(target_product_id)
            if tp:
                if is_young and tp.needs9:
                    requested_product_warning = (
                        f"Программа «{tp.title}» доступна для бизнеса со стажем от 9 месяцев. "
                        f"Для вашей компании подобраны лучшие доступные альтернативы."
                    )
                elif org and org.lower() not in tp.org_types:
                    target_org_name = "ООО" if "ooo" in tp.org_types else "ИП"
                    user_org_name = "ИП" if org.lower() == "ip" else "ООО"
                    requested_product_warning = (
                        f"Программа «{tp.title}» предназначена для {target_org_name}, "
                        f"а для вашей формы ({user_org_name}) подобраны подходящие варианты."
                    )

        # 1. Filter strictly available
        available = [
            p for p in all_products
            if cls.is_strictly_available(p, org, collateral, age_months, age_category, amount, term_months)
        ]

        # 2. If nothing fits strictly (e.g. requested amount or term too high/low, or young business requested restricted product),
        # relaxed match with explanations/warnings
        candidates = available
        if not candidates:
            # Relax amount and term constraints, keep org and age restrictions
            candidates = [
                p for p in all_products
                if (not org or org.lower() in p.org_types) and (not is_young or not p.needs9)
            ]
            if not candidates:
                candidates = all_products

        scored: List[Tuple[float, ProductSpec, List[str], List[str], int, int]] = []

        for p in candidates:
            score = 100.0 - p.rate  # lower rate gets higher base score
            why: List[str] = []
            warnings: List[str] = []

            if requested_product_warning:
                warnings.append(requested_product_warning)

            # Direct product request boost
            if target_product_id and (p.id == target_product_id or p.backend_id == target_product_id):
                score += 80.0
                why.append(f"Вы интересовались программой «{p.title}»")

            # Purpose alignment scoring
            if purpose:
                if purpose == "marketplace":
                    if p.id == "seller":
                        score += 65.0
                        why.append("Идеально для продаж на Wildberries, Ozon и других маркетплейсах")
                elif purpose in ("cashflow_gap", "gap"):
                    if p.id == "overdraft":
                        score += 65.0
                        why.append("Специально для быстрого покрытия кассовых разрывов")
                    elif p.id == "credit-line":
                        score += 45.0
                        why.append("Кредитная линия позволяет брать транши по мере необходимости")
                    elif p.id == "express":
                        score += 35.0
                        why.append("Быстрое получение средств на покрытие кассового разрыва")
                elif purpose == "credit_line":
                    if p.id == "credit-line":
                        score += 65.0
                        why.append("Возобновляемая кредитная линия с траншами по запросу")
                elif purpose == "vehicle":
                    if p.id == "auto":
                        score += 65.0
                        why.append("Целевой автокредит с пониженной ставкой")
                elif purpose == "equipment":
                    if p.id == "equipment":
                        score += 65.0
                        why.append("Целевой кредит на покупку производственного оборудования")
                elif purpose == "real_estate":
                    if p.id in ("mortgage-ooo", "mortgage-ip"):
                        score += 65.0
                        why.append("Специальная ипотечная программа для коммерческой недвижимости")
                    elif p.id == "secured-property":
                        score += 45.0
                        why.append("Крупный кредит под залог существующей недвижимости")
                elif purpose == "business_purchase":
                    if p.id == "ready-business":
                        score += 65.0
                        why.append("Целевое финансирование покупки действующего бизнеса или завода")
                elif purpose == "express":
                    if p.id == "express":
                        score += 65.0
                        why.append("Ускоренное онлайн-одобрение за 2 минуты")
                elif purpose == "small_loan":
                    if p.id == "small":
                        score += 65.0
                        why.append("Быстрый кредит до 1 млн ₽ без залога")
                elif purpose == "working_capital":
                    if p.id in ("working-capital", "ooo-unsecured", "ip-unsecured"):
                        score += 60.0
                        why.append("Оптимально для пополнения оборотных средств и текущих расходов")

            # If user did not specify collateral, give strong preference to unsecured products
            if not collateral:
                if p.collateral == "without":
                    score += 30.0

            # Young business handling
            if is_young:
                if p.young_max:
                    why.append(f"Доступен бизнесу младше 9 месяцев (лимит до {p.young_max // 1_000_000} млн ₽)")
                    if amount and amount > p.young_max:
                        warnings.append(
                            f"Для компаний со стажем до 9 мес. максимальная сумма составляет {p.young_max // 1_000_000} млн ₽"
                        )
                elif not p.needs9:
                    why.append("Доступен молодым компаниям без ограничений по специальному лимиту")

            # Collateral note
            if p.collateral == "without":
                why.append("Без залога и поручительства")
            elif p.collateral == "with":
                why.append("С обеспечением залогом (сниженная ставка)")
            elif p.collateral == "any":
                why.append("Гибкие условия: можно оформить как с залогом, так и без")

            # Compute effective amount and term for this product
            max_limit = cls.max_amount_for(p, age_months, age_category)
            offer_amount = min(max(calc_amount, p.amount[0]), max_limit)
            if calc_amount > max_limit:
                warnings.append(f"Сумма скорректирована до максимума по продукту: {max_limit:,} ₽".replace(",", " "))
            elif calc_amount < p.amount[0]:
                warnings.append(f"Сумма скорректирована до минимальной по продукту: {p.amount[0]:,} ₽".replace(",", " "))

            offer_term = min(max(calc_term, p.term[0]), p.term[1])
            if calc_term > p.term[1]:
                warnings.append(f"Срок ограничен максимальным по продукту: {p.term[1]} мес.")
            elif calc_term < p.term[0]:
                warnings.append(f"Срок увеличен до минимального по продукту: {p.term[0]} мес.")

            scored.append((score, p, why, warnings, offer_amount, offer_term))

        # Sort by score descending
        scored.sort(key=lambda x: x[0], reverse=True)

        offers: List[ProductOffer] = []
        for _, product, why, warnings, offer_amount, offer_term in scored[:limit]:
            payment, overpayment = compute_annuity(offer_amount, product.rate, offer_term)

            offers.append(
                ProductOffer(
                    product=product,
                    amount=offer_amount,
                    term_months=offer_term,
                    payment=payment,
                    overpayment=overpayment,
                    why_matched=why,
                    warnings=warnings,
                )
            )

        return offers
