-- =============================================================================
-- 0002_seed_taxonomy.sql — categories & badges, translated to ar / en / fr
-- =============================================================================
INSERT INTO categories (id, slug, icon, color, sort_order)
VALUES (1, 'quran', '📖', '#0ea5a4', 1),
    (2, 'seerah', '🕌', '#8b5cf6', 2),
    (3, 'prophets', '🌟', '#f59e0b', 3),
    (4, 'fiqh', '⚖️', '#3b82f6', 4),
    (5, 'hadith', '📜', '#ec4899', 5),
    (6, 'history', '🏛️', '#14b8a6', 6),
    (7, 'ramadan', '🌙', '#6366f1', 7),
    (8, 'akhlaq', '🤝', '#22c55e', 8) ON CONFLICT (id) DO NOTHING;
SELECT setval(
        'categories_id_seq',
        (
            SELECT max(id)
            FROM categories
        )
    );
INSERT INTO category_translations (category_id, locale, name, description)
VALUES (
        1,
        'ar',
        'القرآن الكريم',
        'السور والآيات وعلوم القرآن'
    ),
    (
        1,
        'en',
        'The Noble Qur''an',
        'Surahs, verses and Qur''anic sciences'
    ),
    (
        1,
        'fr',
        'Le Saint Coran',
        'Sourates, versets et sciences coraniques'
    ),
    (
        2,
        'ar',
        'السيرة النبوية',
        'حياة النبي محمد ﷺ وغزواته'
    ),
    (
        2,
        'en',
        'Prophetic Biography',
        'The life of Prophet Muhammad ﷺ'
    ),
    (
        2,
        'fr',
        'La Sîra',
        'La vie du Prophète Muhammad ﷺ'
    ),
    (
        3,
        'ar',
        'قصص الأنبياء',
        'الأنبياء والرسل عليهم السلام'
    ),
    (
        3,
        'en',
        'Stories of the Prophets',
        'The prophets and messengers'
    ),
    (
        3,
        'fr',
        'Histoires des Prophètes',
        'Les prophètes et messagers'
    ),
    (
        4,
        'ar',
        'الفقه والعبادات',
        'الصلاة والزكاة والحج والطهارة'
    ),
    (
        4,
        'en',
        'Fiqh & Worship',
        'Prayer, zakat, hajj and purification'
    ),
    (
        4,
        'fr',
        'Fiqh et Adorations',
        'Prière, zakat, hajj et purification'
    ),
    (
        5,
        'ar',
        'الحديث الشريف',
        'أحاديث النبي ﷺ ورواتها'
    ),
    (
        5,
        'en',
        'Hadith',
        'Prophetic traditions and their narrators'
    ),
    (
        5,
        'fr',
        'Le Hadith',
        'Traditions prophétiques et leurs rapporteurs'
    ),
    (
        6,
        'ar',
        'التاريخ الإسلامي',
        'الخلفاء والدول والفتوحات'
    ),
    (
        6,
        'en',
        'Siraj History',
        'Caliphs, dynasties and conquests'
    ),
    (
        6,
        'fr',
        'Histoire Islamique',
        'Califes, dynasties et conquêtes'
    ),
    (
        7,
        'ar',
        'رمضان والصيام',
        'أحكام الصيام وليلة القدر'
    ),
    (
        7,
        'en',
        'Ramadan & Fasting',
        'Rulings of fasting and Laylat al-Qadr'
    ),
    (
        7,
        'fr',
        'Ramadan et Jeûne',
        'Règles du jeûne et Laylat al-Qadr'
    ),
    (
        8,
        'ar',
        'الأخلاق والآداب',
        'حسن الخلق وآداب المسلم'
    ),
    (
        8,
        'en',
        'Manners & Ethics',
        'Good character and Siraj etiquette'
    ),
    (
        8,
        'fr',
        'Morale et Éthique',
        'Bon caractère et bienséance islamique'
    ) ON CONFLICT (category_id, locale) DO NOTHING;
INSERT INTO badges (id, slug, icon, threshold, kind)
VALUES (1, 'first_steps', '🌱', 1, 'games'),
    (2, 'regular', '🎯', 10, 'games'),
    (3, 'devoted', '🔥', 50, 'games'),
    (4, 'flawless', '💎', 1, 'perfect'),
    (5, 'streak_master', '⚡', 10, 'streak'),
    (6, 'seeker', '📚', 500, 'xp'),
    (7, 'scholar', '🎓', 2500, 'xp'),
    (8, 'companion', '🤝', 5, 'friends'),
    (9, 'duelist', '⚔️', 10, 'duels') ON CONFLICT (id) DO NOTHING;
SELECT setval(
        'badges_id_seq',
        (
            SELECT max(id)
            FROM badges
        )
    );
INSERT INTO badge_translations (badge_id, locale, name, description)
VALUES (1, 'ar', 'أول خطوة', 'أكملت أول جولة لك'),
    (
        1,
        'en',
        'First Steps',
        'Completed your first round'
    ),
    (
        1,
        'fr',
        'Premiers Pas',
        'Première partie terminée'
    ),
    (2, 'ar', 'مواظب', 'أكملت ١٠ جولات'),
    (2, 'en', 'Regular', 'Completed 10 rounds'),
    (2, 'fr', 'Régulier', '10 parties terminées'),
    (3, 'ar', 'مجتهد', 'أكملت ٥٠ جولة'),
    (3, 'en', 'Devoted', 'Completed 50 rounds'),
    (3, 'fr', 'Assidu', '50 parties terminées'),
    (4, 'ar', 'إجابة كاملة', 'جولة كاملة بدون خطأ'),
    (
        4,
        'en',
        'Flawless',
        'A full round without a single mistake'
    ),
    (4, 'fr', 'Sans Faute', 'Une partie parfaite'),
    (
        5,
        'ar',
        'سلسلة ذهبية',
        'عشر إجابات صحيحة متتالية'
    ),
    (
        5,
        'en',
        'Streak Master',
        'Ten correct answers in a row'
    ),
    (
        5,
        'fr',
        'Série Parfaite',
        'Dix bonnes réponses d''affilée'
    ),
    (6, 'ar', 'طالب علم', 'جمعت ٥٠٠ نقطة خبرة'),
    (6, 'en', 'Seeker', 'Earned 500 XP'),
    (6, 'fr', 'Chercheur', '500 XP accumulés'),
    (7, 'ar', 'عالِم', 'جمعت ٢٥٠٠ نقطة خبرة'),
    (7, 'en', 'Scholar', 'Earned 2500 XP'),
    (7, 'fr', 'Érudit', '2500 XP accumulés'),
    (8, 'ar', 'رفيق', 'كوّنت خمس صداقات'),
    (8, 'en', 'Companion', 'Made five friends'),
    (8, 'fr', 'Compagnon', 'Cinq amis ajoutés'),
    (9, 'ar', 'مبارز', 'فزت بعشرة تحديات'),
    (9, 'en', 'Duelist', 'Won ten challenges'),
    (9, 'fr', 'Duelliste', 'Dix défis remportés') ON CONFLICT (badge_id, locale) DO NOTHING;