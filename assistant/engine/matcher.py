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
        limit: int = 3,
    ) -> List[ProductOffer]:
        all_products = get_all_products()

        # Defaults for financial calculation
        calc_amount = amount if amount and amount > 0 else 1_000_000
        calc_term = term_months if term_months and term_months > 0 else 12

        is_young = (age_months is not None and age_months < 9) or (age_category == "lt9")

        # 1. Filter strictly available
        available = [
            p for p in all_products
            if cls.is_strictly_available(p, org, collateral, age_months, age_category, amount, term_months)
        ]

        # 2. If nothing fits strictly (e.g. requested amount or term too high/low, or young business requested restricted product),
        # relaxed match with explanations/warnings
        relaxed = False
        candidates = available
        if not candidates:
            relaxed = True
            # Relax amount and term constraints, keep org and age restrictions
            candidates = [
                p for p in all_products
                if (not org or org.lower() in p.org_types) and (not is_young or not p.needs9)
            ]
            if not candidates:
                # If still empty (e.g. org doesn't match anything), fallback to all
                candidates = all_products

        scored: List[Tuple[float, ProductSpec, List[str], List[str]]] = []

        for p in candidates:
            score = 100.0 - p.rate  # lower rate gets higher base score
            why: List[str] = []
            warnings: List[str] = []

            # Purpose alignment scoring
            if purpose:
                if purpose == "marketplace":
                    if p.id == "seller":
                        score += 60.0
                        why.append("Идеально для продаж на Wildberries, Ozon и других маркетплейсах")
                elif purpose in ("cashflow_gap", "gap"):
                    if p.id == "overdraft":
                        score += 60.0
                        why.append("Специально для быстрого покрытия кассовых разрывов")
                    elif p.id == "credit-line":
                        score += 40.0
                        why.append("Кредитная линия позволяет брать транши по мере необходимости")
                elif purpose == "vehicle":
                    if p.id == "auto":
                        score += 60.0
                        why.append("Целевой автокредит с пониженной ставкой")
                elif purpose == "equipment":
                    if p.id == "equipment":
                        score += 60.0
                        why.append("Целевой кредит на покупку производственного оборудования")
                elif purpose == "real_estate":
                    if p.id in ("mortgage-ooo", "mortgage-ip"):
                        score += 60.0
                        why.append("Специальная ипотечная программа для коммерческой недвижимости")
                    elif p.id == "secured-property":
                        score += 40.0
                        why.append("Крупный кредит под залог существующей недвижимости")
                elif purpose == "business_purchase":
                    if p.id == "ready-business":
                        score += 60.0
                        why.append("Целевое финансирование покупки действующего бизнеса или завода")
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

            # Check clamp for annuity
            offer_amount = calc_amount
            max_limit = cls.max_amount_for(p, age_months, age_category)
            if offer_amount > max_limit:
                offer_amount = max_limit
                warnings.append(f"Сумма скорректирована до максимума по продукту: {max_limit:,} ₽")
            elif offer_amount < p.amount[0]:
                offer_amount = p.amount[0]
                warnings.append(f"Сумма скорректирована до минимальной по продукту: {p.amount[0]:,} ₽")

            offer_term = calc_term
            if offer_term > p.term[1]:
                offer_term = p.term[1]
                warnings.append(f"Срок ограничен максимальным по продукту: {p.term[1]} мес.")
            elif offer_term < p.term[0]:
                offer_term = p.term[0]
                warnings.append(f"Срок увеличен до минимального по продукту: {p.term[0]} мес.")

            payment, overpayment = compute_annuity(offer_amount, p.rate, offer_term)

            scored.append((score, p, why, warnings))

        # Sort by score descending
        scored.sort(key=lambda x: x[0], reverse=True)

        offers: List[ProductOffer] = []
        for _, product, why, warnings in scored[:limit]:
            offer_amount = min(max(calc_amount, product.amount[0]), cls.max_amount_for(product, age_months, age_category))
            offer_term = min(max(calc_term, product.term[0]), product.term[1])
            payment, overpayment = compute_annuity(offer_amount, product.rate, offer_term)

            offers.append(
                ProductOffer(
                    product=product,
                    payment=payment,
                    overpayment=overpayment,
                    why_matched=why,
                    warnings=warnings,
                )
            )

        return offers
