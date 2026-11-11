/* Sirāj v2 prototype data: domains, categories, questions (AR/EN/FR), people, interface strings.
   Everything here is sample data; in the app it comes from the API. */
window.SJ = window.SJ || {};

SJ.DOMAINS = [
  { id: "islamic", name: { en: "Islamic Knowledge", ar: "العلوم الإسلامية", fr: "Savoir islamique" }, desc: { en: "Qur'an, Sunnah, history and practice", ar: "القرآن والسنة والتاريخ والعبادات", fr: "Coran, Sunna, histoire et pratique" }, icon: "mosque", color: "#0b7a5c" },
  { id: "arabic", name: { en: "Arabic & Qur'anic Sciences", ar: "علوم العربية والقرآن", fr: "Arabe et sciences coraniques" }, desc: { en: "Grammar, words and eloquence", ar: "النحو والصرف والمفردات والبلاغة", fr: "Grammaire, vocabulaire et éloquence" }, icon: "pen", color: "#2f5f9e", isNew: true }
];

SJ.CATS = [
  { id: "quran", domain: "islamic", icon: "book", color: "#0d9488", name: { en: "The Noble Qur'an", ar: "القرآن الكريم", fr: "Le Noble Coran" }, mastery: 64 },
  { id: "seerah", domain: "islamic", icon: "mosque", color: "#7c3aed", name: { en: "Prophetic Biography", ar: "السيرة النبوية", fr: "Biographie prophétique" }, mastery: 58 },
  { id: "prophets", domain: "islamic", icon: "star", color: "#c2410c", name: { en: "Stories of the Prophets", ar: "قصص الأنبياء", fr: "Histoires des prophètes" }, mastery: 81 },
  { id: "fiqh", domain: "islamic", icon: "scale", color: "#2563eb", name: { en: "Fiqh & Worship", ar: "الفقه والعبادات", fr: "Fiqh et adoration" }, mastery: 47 },
  { id: "hadith", domain: "islamic", icon: "scroll", color: "#db2777", name: { en: "Hadith", ar: "الحديث", fr: "Hadith" }, mastery: 39 },
  { id: "history", domain: "islamic", icon: "columns", color: "#0f766e", name: { en: "Islamic History", ar: "التاريخ الإسلامي", fr: "Histoire islamique" }, mastery: 52 },
  { id: "ramadan", domain: "islamic", icon: "moon", color: "#4f46e5", name: { en: "Ramadan & Fasting", ar: "رمضان والصيام", fr: "Ramadan et jeûne" }, mastery: 70 },
  { id: "akhlaq", domain: "islamic", icon: "heart", color: "#059669", name: { en: "Manners & Ethics", ar: "الأخلاق والآداب", fr: "Éthique et bonnes manières" }, mastery: 44 },
  { id: "custom", domain: "custom", icon: "pen", color: "#a16207", name: { en: "My questions", ar: "أسئلتي", fr: "Mes questions" }, mastery: 0, hidden: true },
  { id: "nahw", domain: "arabic", icon: "type", color: "#2f5f9e", name: { en: "Grammar (Naḥw)", ar: "النحو", fr: "Grammaire (naḥw)" }, mastery: 22 },
  { id: "vocab", domain: "arabic", icon: "languages", color: "#b45309", name: { en: "Vocabulary", ar: "المفردات", fr: "Vocabulaire" }, mastery: 35 },
  { id: "sarf", domain: "arabic", icon: "puzzle", color: "#7c3aed", name: { en: "Morphology (Ṣarf)", ar: "الصرف", fr: "Morphologie (ṣarf)" }, mastery: 10 },
  { id: "balagha", domain: "arabic", icon: "feather", color: "#be185d", name: { en: "Eloquence (Balāgha)", ar: "البلاغة", fr: "Éloquence (balāgha)" }, mastery: 0 }
];

/* q: question, a: answers (same order in every language), c: index of the correct answer, x: explanation */
SJ.QUESTIONS = [
  { id: 101, cat: "quran", lvl: 1, c: 1, src: "Mushaf order",
    q: { en: "How many surahs are in the Qur'an?", ar: "كم عدد سور القرآن الكريم؟", fr: "Combien de sourates compte le Coran ?" },
    a: { en: ["112", "114", "116", "120"], ar: ["112", "114", "116", "120"], fr: ["112", "114", "116", "120"] },
    x: { en: "The Qur'an has 114 surahs, from al-Fātiḥah to an-Nās.", ar: "في القرآن الكريم 114 سورة، من الفاتحة إلى الناس.", fr: "Le Coran compte 114 sourates, d'al-Fâtiha à an-Nâs." } },
  { id: 104, cat: "quran", lvl: 2, c: 1, src: "Qur'an 9; 27:30",
    q: { en: "Which surah does not begin with the Basmala?", ar: "ما السورة التي لا تبدأ بالبسملة؟", fr: "Quelle sourate ne commence pas par la Basmala ?" },
    a: { en: ["Al-Kahf", "At-Tawbah", "Al-Ikhlāṣ", "Yā-Sīn"], ar: ["الكهف", "التوبة", "الإخلاص", "يس"], fr: ["Al-Kahf", "At-Tawba", "Al-Ikhlâs", "Yâ-Sîn"] },
    x: { en: "At-Tawbah (9) is the only surah that opens without the Basmala. It appears twice in an-Naml, once at its opening and once in verse 27:30.", ar: "سورة التوبة هي السورة الوحيدة التي لا تبدأ بالبسملة، وقد وردت البسملة مرتين في سورة النمل.", fr: "At-Tawba (9) est la seule sourate qui ne s'ouvre pas par la Basmala ; celle-ci apparaît deux fois dans an-Naml (27:30)." } },
  { id: 106, cat: "quran", lvl: 2, c: 2, src: "Qur'an 2",
    q: { en: "Which is the longest surah of the Qur'an?", ar: "ما أطول سورة في القرآن الكريم؟", fr: "Quelle est la plus longue sourate du Coran ?" },
    a: { en: ["Āl ʿImrān", "An-Nisāʾ", "Al-Baqarah", "Al-Māʾidah"], ar: ["آل عمران", "النساء", "البقرة", "المائدة"], fr: ["Âl 'Imrân", "An-Nisâ'", "Al-Baqara", "Al-Mâ'ida"] },
    x: { en: "Al-Baqarah is the longest surah, with 286 verses.", ar: "سورة البقرة أطول سور القرآن، وآياتها 286 آية.", fr: "Al-Baqara est la plus longue sourate, avec 286 versets." } },
  { id: 203, cat: "seerah", lvl: 1, c: 1, src: "Ibn Hishām, al-Sīra al-Nabawiyya",
    q: { en: "In which year CE did the Hijra to Madinah take place?", ar: "في أي سنة ميلادية كانت الهجرة إلى المدينة؟", fr: "En quelle année a eu lieu l'Hégire vers Médine ?" },
    a: { en: ["610", "622", "630", "632"], ar: ["610", "622", "630", "632"], fr: ["610", "622", "630", "632"] },
    x: { en: "The Hijra in 622 CE marks year 1 of the Islamic calendar.", ar: "كانت الهجرة سنة 622م، وبها بدأ التقويم الهجري.", fr: "L'Hégire, en 622, marque l'an 1 du calendrier islamique." } },
  { id: 205, cat: "seerah", lvl: 2, c: 1, src: "Sunan Abī Dāwūd 499",
    q: { en: "Who was the first to call the adhān in Madinah?", ar: "من أول من أذّن في المدينة؟", fr: "Qui fut le premier à appeler à la prière (adhân) à Médine ?" },
    a: { en: ["Abdullah ibn Zayd", "Bilal ibn Rabah", "Umar ibn al-Khattab", "Abu Mahdhurah"], ar: ["عبد الله بن زيد", "بلال بن رباح", "عمر بن الخطاب", "أبو محذورة"], fr: ["Abdallah ibn Zayd", "Bilal ibn Rabah", "Omar ibn al-Khattab", "Abou Mahdhoura"] },
    x: { en: "Abdullah ibn Zayd saw the words of the adhān in a dream. The Prophet ﷺ asked him to teach them to Bilal, whose voice carried further.", ar: "رأى عبد الله بن زيد كلمات الأذان في المنام، فأمره النبي ﷺ أن يلقيها على بلال لأنه أندى صوتًا.", fr: "Abdallah ibn Zayd vit les paroles de l'adhân en rêve ; le Prophète ﷺ lui demanda de les enseigner à Bilal, dont la voix portait plus loin." } },
  { id: 207, cat: "seerah", lvl: 1, c: 0, src: "Ibn Hishām, al-Sīra al-Nabawiyya",
    q: { en: "What was the name of the Prophet's ﷺ mother?", ar: "ما اسم أم النبي ﷺ؟", fr: "Comment s'appelait la mère du Prophète ﷺ ?" },
    a: { en: ["Aminah bint Wahb", "Khadijah bint Khuwaylid", "Halimah al-Saʿdiyyah", "Fatimah bint Asad"], ar: ["آمنة بنت وهب", "خديجة بنت خويلد", "حليمة السعدية", "فاطمة بنت أسد"], fr: ["Amina bint Wahb", "Khadija bint Khuwaylid", "Halima as-Sa'diyya", "Fatima bint Asad"] },
    x: { en: "His mother was Aminah bint Wahb. Halimah al-Saʿdiyyah was his wet nurse.", ar: "أمه آمنة بنت وهب، أما حليمة السعدية فكانت مرضعته.", fr: "Sa mère était Amina bint Wahb ; Halima as-Sa'diyya fut sa nourrice." } },
  { id: 303, cat: "prophets", lvl: 1, c: 0, src: "Qur'an 4:125",
    q: { en: "Which prophet carries the title “Khalil Allah”?", ar: "أي نبي لُقّب بخليل الله؟", fr: "Quel prophète porte le titre de « Khalil Allah » ?" },
    a: { en: ["Ibrahim", "Musa", "Isa", "Nuh"], ar: ["إبراهيم", "موسى", "عيسى", "نوح"], fr: ["Ibrahim", "Moussa", "Issa", "Nouh"] },
    x: { en: "“And Allah took Ibrahim as an intimate friend” (4:125).", ar: "قال تعالى: «واتخذ الله إبراهيم خليلا» (النساء: 125).", fr: "« Et Allah a pris Ibrahim pour ami intime » (4:125)." } },
  { id: 305, cat: "prophets", lvl: 1, c: 0, src: "Qur'an 37:139–148",
    q: { en: "Which prophet was swallowed by a whale?", ar: "أي نبي التقمه الحوت؟", fr: "Quel prophète fut avalé par un poisson géant ?" },
    a: { en: ["Yunus", "Ayyub", "Yusuf", "Musa"], ar: ["يونس", "أيوب", "يوسف", "موسى"], fr: ["Younous", "Ayyoub", "Youssouf", "Moussa"] },
    x: { en: "Yunus was swallowed by the whale and glorified Allah in its belly (37:139–148).", ar: "التقم الحوتُ يونسَ عليه السلام فسبّح الله في بطنه (الصافات: 139–148).", fr: "Younous fut avalé par le poisson et glorifia Allah dans son ventre (37:139-148)." } },
  { id: 306, cat: "prophets", lvl: 2, c: 0, src: "Qur'an 4:163",
    q: { en: "Which prophet was given the Zabur (Psalms)?", ar: "أي نبي أوتي الزبور؟", fr: "À quel prophète fut donné le Zabour (les Psaumes) ?" },
    a: { en: ["Dawud", "Sulayman", "Musa", "Isa"], ar: ["داود", "سليمان", "موسى", "عيسى"], fr: ["Dawoud", "Soulayman", "Moussa", "Issa"] },
    x: { en: "“And to Dawud We gave the Zabur” (4:163).", ar: "قال تعالى: «وآتينا داود زبورا» (النساء: 163).", fr: "« Et Nous avons donné le Zabour à Dawoud » (4:163)." } },
  { id: 401, cat: "fiqh", lvl: 1, c: 1, src: "Ṣaḥīḥ al-Bukhārī 8",
    q: { en: "How many pillars does Islam have?", ar: "كم عدد أركان الإسلام؟", fr: "Combien y a-t-il de piliers de l'Islam ?" },
    a: { en: ["4", "5", "6", "3"], ar: ["4", "5", "6", "3"], fr: ["4", "5", "6", "3"] },
    x: { en: "“Islam is built on five”: the testimony of faith, prayer, zakat, Hajj and fasting Ramadan.", ar: "«بُني الإسلام على خمس»: الشهادتان والصلاة والزكاة والحج وصوم رمضان.", fr: "« L'islam est bâti sur cinq » : l'attestation de foi, la prière, la zakat, le hajj et le jeûne du Ramadan." } },
  { id: 403, cat: "fiqh", lvl: 1, c: 0, src: "Agreed by the four schools",
    q: { en: "How many obligatory rakʿahs are in the Fajr prayer?", ar: "كم عدد ركعات صلاة الفجر المفروضة؟", fr: "Combien de rak'as compte la prière obligatoire du Fajr ?" },
    a: { en: ["2", "3", "4", "1"], ar: ["2", "3", "4", "1"], fr: ["2", "3", "4", "1"] },
    x: { en: "The obligatory Fajr prayer is two rakʿahs.", ar: "صلاة الفجر المفروضة ركعتان.", fr: "La prière obligatoire du Fajr compte deux rak'as." } },
  { id: 412, cat: "fiqh", lvl: 3, c: 0, src: "Sunan Abī Dāwūd 1573",
    q: { en: "What is the nisab of gold for zakat according to the majority?", ar: "ما نصاب الذهب في الزكاة عند الجمهور؟", fr: "Quel est le nissab de l'or pour la zakat selon la majorité ?" },
    a: { en: ["20 mithqal (about 85 g)", "10 mithqal", "40 mithqal", "200 dirhams"], ar: ["عشرون مثقالًا (نحو 85 غرامًا)", "عشرة مثاقيل", "أربعون مثقالًا", "مئتا درهم"], fr: ["20 mithqals (environ 85 g)", "10 mithqals", "40 mithqals", "200 dirhams"] },
    x: { en: "The majority set it at 20 mithqal, commonly estimated at about 85 grams. Modern estimates vary slightly.", ar: "حدده الجمهور بعشرين مثقالًا، وتقدّر عادة بنحو 85 غرامًا مع اختلاف يسير في التقدير.", fr: "La majorité le fixe à 20 mithqals, estimés à environ 85 g ; les estimations modernes varient légèrement." } },
  { id: 502, cat: "hadith", lvl: 1, c: 0, src: "Al-Bukhārī d. 256 AH",
    q: { en: "Who compiled Ṣaḥīḥ al-Bukhārī?", ar: "من جمع صحيح البخاري؟", fr: "Qui a compilé le Sahih al-Boukhari ?" },
    a: { en: ["Muhammad ibn Ismaʿil al-Bukhari", "Muslim ibn al-Hajjaj", "Abu Dawud al-Sijistani", "Muhammad ibn ʿIsa al-Tirmidhi"], ar: ["محمد بن إسماعيل البخاري", "مسلم بن الحجاج", "أبو داود السجستاني", "محمد بن عيسى الترمذي"], fr: ["Muhammad ibn Isma'il al-Boukhari", "Mouslim ibn al-Hajjaj", "Abou Dawoud as-Sijistani", "Muhammad ibn 'Issa at-Tirmidhi"] },
    x: { en: "Imam Muhammad ibn Ismaʿil al-Bukhari (d. 256 AH) compiled it over about sixteen years.", ar: "جمعه الإمام محمد بن إسماعيل البخاري (ت 256هـ) في نحو ست عشرة سنة.", fr: "L'imam Muhammad ibn Isma'il al-Boukhari (m. 256 H) l'a compilé en seize ans environ." } },
  { id: 505, cat: "hadith", lvl: 2, c: 0, src: "Ṣaḥīḥ al-Bukhārī 1",
    q: { en: "Which collection opens with “Actions are only by intentions”?", ar: "أي كتاب يُفتتح بحديث «إنما الأعمال بالنيات»؟", fr: "Quel recueil s'ouvre par « Les actes ne valent que par les intentions » ?" },
    a: { en: ["Ṣaḥīḥ al-Bukhārī", "Sunan Abī Dāwūd", "Muwaṭṭaʾ Mālik", "Sunan an-Nasāʾī"], ar: ["صحيح البخاري", "سنن أبي داود", "موطأ مالك", "سنن النسائي"], fr: ["Sahih al-Boukhari", "Sounan Abi Dawoud", "Mouwatta Malik", "Sounan an-Nasa'i"] },
    x: { en: "Al-Bukhari placed the hadith of intention first; it is hadith number 1.", ar: "افتتح البخاري صحيحه بحديث النية، وهو الحديث رقم 1.", fr: "Al-Boukhari a placé le hadith de l'intention en premier : c'est le hadith n° 1." } },
  { id: 604, cat: "history", lvl: 2, c: 0, src: "Al-Ṭabarī, Tārīkh",
    q: { en: "What was the capital of the Umayyad caliphate?", ar: "ما عاصمة الخلافة الأموية؟", fr: "Quelle était la capitale du califat omeyyade ?" },
    a: { en: ["Damascus", "Baghdad", "Cairo", "Cordoba"], ar: ["دمشق", "بغداد", "القاهرة", "قرطبة"], fr: ["Damas", "Bagdad", "Le Caire", "Cordoue"] },
    x: { en: "Damascus was the Umayyad capital from 661 to 750. The Abbasids later founded Baghdad.", ar: "كانت دمشق عاصمة الأمويين من 661 إلى 750م، ثم أسس العباسيون بغداد.", fr: "Damas fut la capitale omeyyade de 661 à 750 ; les Abbassides fondèrent ensuite Bagdad." } },
  { id: 610, cat: "history", lvl: 2, c: 0, src: "Al-Bakrī, al-Masālik wa-l-Mamālik",
    q: { en: "Which city did ʿUqba ibn Nafiʿ found in 670 CE?", ar: "أي مدينة أسسها عقبة بن نافع سنة 670م؟", fr: "Quelle ville 'Uqba ibn Nafi' fonda-t-il en 670 ?" },
    a: { en: ["Kairouan", "Fez", "Tunis", "Cordoba"], ar: ["القيروان", "فاس", "تونس", "قرطبة"], fr: ["Kairouan", "Fès", "Tunis", "Cordoue"] },
    x: { en: "ʿUqba ibn Nafiʿ founded Kairouan as the base of the Muslims in Ifriqiya.", ar: "أسس عقبة بن نافع القيروان لتكون قاعدة للمسلمين في إفريقية.", fr: "'Uqba ibn Nafi' fonda Kairouan comme base des musulmans en Ifriqiya." } },
  { id: 612, cat: "history", lvl: 3, c: 0, src: "Al-Maqrīzī, al-Sulūk",
    q: { en: "In which battle was the Mongol advance stopped in 1260?", ar: "في أي معركة أُوقف زحف المغول سنة 1260م؟", fr: "Dans quelle bataille l'avancée mongole fut-elle stoppée en 1260 ?" },
    a: { en: ["ʿAyn Jālūt", "Ḥaṭṭīn", "Manzikert", "Talas"], ar: ["عين جالوت", "حطين", "ملاذكرد", "طلاس"], fr: ["Aïn Djalout", "Hattin", "Manzikert", "Talas"] },
    x: { en: "The Mamluks under Quṭuz and Baybars defeated the Mongols at ʿAyn Jālūt in Palestine.", ar: "هزم المماليك بقيادة قطز وبيبرس المغولَ في عين جالوت بفلسطين.", fr: "Les Mamelouks de Qutuz et Baybars vainquirent les Mongols à Aïn Djalout, en Palestine." } },
  { id: 703, cat: "ramadan", lvl: 1, c: 1, src: "Qur'an 97:3",
    q: { en: "Laylat al-Qadr is better than how many months?", ar: "ليلة القدر خير من كم شهر؟", fr: "Laylat al-Qadr est meilleure que combien de mois ?" },
    a: { en: ["100", "1,000", "70", "12"], ar: ["100", "1000", "70", "12"], fr: ["100", "1 000", "70", "12"] },
    x: { en: "“The Night of Decree is better than a thousand months” (97:3).", ar: "قال تعالى: «ليلة القدر خير من ألف شهر» (القدر: 3).", fr: "« La Nuit du Destin est meilleure que mille mois » (97:3)." } },
  { id: 705, cat: "ramadan", lvl: 1, c: 0, src: "Hijri calendar",
    q: { en: "Which month comes after Ramadan?", ar: "ما الشهر الذي يلي رمضان؟", fr: "Quel mois suit le Ramadan ?" },
    a: { en: ["Shawwal", "Shaʿban", "Dhu al-Hijjah", "Rajab"], ar: ["شوال", "شعبان", "ذو الحجة", "رجب"], fr: ["Chawwal", "Cha'ban", "Dhou al-Hijja", "Rajab"] },
    x: { en: "Shawwal follows Ramadan; its first day is Eid al-Fitr.", ar: "يلي رمضانَ شهرُ شوال، وأول أيامه عيد الفطر.", fr: "Chawwal suit le Ramadan ; son premier jour est l'Aïd al-Fitr." } },
  { id: 801, cat: "akhlaq", lvl: 1, c: 0, src: "Qur'an 31:13–19",
    q: { en: "Which surah records Luqman's advice to his son?", ar: "أي سورة ذكرت وصايا لقمان لابنه؟", fr: "Quelle sourate rapporte les conseils de Louqman à son fils ?" },
    a: { en: ["Luqman", "Yusuf", "Maryam", "Al-Kahf"], ar: ["لقمان", "يوسف", "مريم", "الكهف"], fr: ["Louqman", "Youssouf", "Maryam", "Al-Kahf"] },
    x: { en: "Surah Luqman (31:13–19): worship Allah alone, honour your parents, pray, and walk with humility.", ar: "في سورة لقمان (13–19): التوحيد، وبر الوالدين، وإقامة الصلاة، والتواضع.", fr: "Sourate Louqman (31:13-19) : l'unicité d'Allah, la bonté envers les parents, la prière et l'humilité." } },
  { id: 802, cat: "akhlaq", lvl: 2, c: 0, src: "Ṣaḥīḥ al-Bukhārī 6114",
    q: { en: "“The strong one is not the one who wrestles others down, but the one who…”", ar: "«ليس الشديد بالصُّرَعة، إنما الشديد الذي…»", fr: "« Le fort n'est pas celui qui terrasse les autres, mais celui qui… »" },
    a: { en: ["controls himself when angry", "is never defeated", "prays all night", "gives the most charity"], ar: ["يملك نفسه عند الغضب", "لا يُهزم أبدًا", "يقوم الليل كله", "يكثر الصدقة"], fr: ["se maîtrise dans la colère", "n'est jamais vaincu", "prie toute la nuit", "donne le plus d'aumône"] },
    x: { en: "The Prophet ﷺ defined strength as self-control in anger.", ar: "جعل النبي ﷺ القوة الحقيقية في ملك النفس عند الغضب.", fr: "Le Prophète ﷺ définit la force comme la maîtrise de soi dans la colère." } },
  { id: 901, cat: "nahw", lvl: 1, c: 0, src: "Al-Ājurrūmiyya",
    q: { en: "In «ذَهَبَ الوَلَدُ», what is «الوَلَدُ»?", ar: "ما إعراب «الولدُ» في «ذهبَ الولدُ»؟", fr: "Dans « ذَهَبَ الوَلَدُ », quelle est la fonction de « الوَلَدُ » ?" },
    a: { en: ["Subject (fāʿil)", "Object (mafʿūl bihi)", "Topic (mubtadaʾ)", "Predicate (khabar)"], ar: ["فاعل", "مفعول به", "مبتدأ", "خبر"], fr: ["Sujet (fā'il)", "Complément d'objet (maf'ūl bihi)", "Thème (mubtada')", "Prédicat (khabar)"] },
    x: { en: "It follows a verb and does the action, so it is a fāʿil, marked with ḍamma.", ar: "اسم جاء بعد فعل وقام بالفعل، فهو فاعل مرفوع وعلامة رفعه الضمة.", fr: "Il suit un verbe et accomplit l'action : c'est un fā'il, au nominatif (ḍamma)." } },
  { id: 921, cat: "vocab", lvl: 1, c: 0, src: "Lisān al-ʿArab",
    q: { en: "What is the plural of «كِتاب» (book)?", ar: "ما جمع «كِتاب»؟", fr: "Quel est le pluriel de « كِتاب » (livre) ?" },
    a: { en: ["كُتُب", "كِتابات", "أكتاب", "مَكاتِب"], ar: ["كُتُب", "كِتابات", "أكتاب", "مَكاتِب"], fr: ["كُتُب", "كِتابات", "أكتاب", "مَكاتِب"] },
    x: { en: "كُتُب is its broken plural. مَكاتِب is the plural of مَكتَب (desk, office).", ar: "جمعه «كُتُب» جمع تكسير، أما «مكاتب» فجمع «مكتب».", fr: "كُتُب est son pluriel brisé ; مَكاتِب est le pluriel de مَكتَب (bureau)." } },
  { id: 931, cat: "sarf", lvl: 2, c: 0, src: "Root ك ت ب",
    q: { en: "What is the root of «مَكْتَبَة» (library)?", ar: "ما جذر كلمة «مكتبة»؟", fr: "Quelle est la racine de « مَكْتَبَة » (bibliothèque) ?" },
    a: { en: ["ك ت ب", "م ك ت", "ت ب ة", "ك ت ة"], ar: ["ك ت ب", "م ك ت", "ت ب ة", "ك ت ة"], fr: ["ك ت ب", "م ك ت", "ت ب ة", "ك ت ة"] },
    x: { en: "Words built on ك ت ب relate to writing: كَتَبَ، كاتِب، مَكتوب، مَكتَبة.", ar: "الكلمات المبنية على «ك ت ب» تدور حول الكتابة: كتب، كاتب، مكتوب، مكتبة.", fr: "Les mots construits sur ك ت ب touchent à l'écriture : كتب، كاتب، مكتوب، مكتبة." } },
  { id: 941, cat: "balagha", lvl: 2, c: 0, src: "Al-Jurjānī, Asrār al-Balāgha",
    q: { en: "«رأيتُ أسدًا يخطب» (I saw a lion giving a speech) is an example of:", ar: "عبارة «رأيتُ أسدًا يخطب» مثال على:", fr: "« رأيتُ أسدًا يخطب » (j'ai vu un lion faire un discours) est un exemple de :" },
    a: { en: ["Metaphor (istiʿāra)", "Simile (tashbīh)", "Paronomasia (jinās)", "Antithesis (ṭibāq)"], ar: ["استعارة", "تشبيه", "جناس", "طباق"], fr: ["Métaphore (isti'āra)", "Comparaison (tashbīh)", "Paronomase (jinās)", "Antithèse (ṭibāq)"] },
    x: { en: "The speaker is called a lion with no word of comparison, so it is an istiʿāra. With “like” it would be a tashbīh.", ar: "سُمّي الخطيب أسدًا دون أداة تشبيه، فهي استعارة، ولو قيل «كالأسد» لكان تشبيهًا.", fr: "L'orateur est appelé lion sans outil de comparaison : c'est une isti'āra. Avec « comme », ce serait un tashbīh." } }
];

SJ.QOTD = 205;

SJ.ME = { name: "Aisha T.", first: "Aisha", firstAr: "عائشة", handle: "aisha_t", initials: "A", initialsAr: "ع", level: 12, xp: 2340, xpNext: 3000, streak: 12, accuracy: 74, rank: 18, games: 412, joined: "March 2026", goal: 5, today: 3, color: "#0e8a6c", weekPts: 1720 };

SJ.PEOPLE = [
  { id: "omar", name: "Omar K.", ar: "عمر ك.", ini: "OK", c: "#3365a8", level: 14, online: true, status: "Playing Hadith now", week: 1980, all: 9120 },
  { id: "khadija", name: "Khadija M.", ar: "خديجة م.", ini: "KM", c: "#0f766e", level: 16, online: true, status: "Online", week: 2140, all: 11380 },
  { id: "youssef", name: "Youssef A.", ar: "يوسف أ.", ini: "YA", c: "#8a4fbf", level: 10, online: false, status: "Played 20 min ago", week: 1410, all: 6040 },
  { id: "sarah", name: "Sarah M.", ar: "سارة م.", ini: "SM", c: "#b4372c", level: 8, online: false, status: "Yesterday", week: 1180, all: 4210 },
  { id: "hamza", name: "Hamza B.", ar: "حمزة ب.", ini: "HB", c: "#c0612b", level: 21, online: true, status: "Online", week: 4820, all: 30450 },
  { id: "leila", name: "Leila H.", ar: "ليلى ح.", ini: "LH", c: "#475569", level: 19, online: false, status: "2 h ago", week: 4410, all: 27100 },
  { id: "ibrahim", name: "Ibrahim D.", ar: "إبراهيم د.", ini: "ID", c: "#be185d", level: 18, online: false, status: "Today", week: 3990, all: 22300 },
  { id: "mariam", name: "Mariam S.", ar: "مريم س.", ini: "MS", c: "#4f46e5", level: 17, online: true, status: "Online", week: 3760, all: 20900 },
  { id: "bilal", name: "Bilal R.", ar: "بلال ر.", ini: "BR", c: "#0d9488", level: 9, online: false, status: "3 days ago", week: 640, all: 3900 },
  { id: "nour", name: "Nour E.", ar: "نور إ.", ini: "NE", c: "#a16207", level: 6, online: true, status: "Online", week: 0, all: 1200 }
];
SJ.FRIENDS = ["omar", "khadija", "youssef", "sarah"];
SJ.REQUESTS = ["bilal", "nour"];

SJ.ACHIEVEMENTS = [
  { id: "first", name: "First steps", d: "Finish your first game", on: true },
  { id: "streak7", name: "Week of light", d: "7-day streak", on: true },
  { id: "perfect", name: "Flawless", d: "10 / 10 in one game", on: true },
  { id: "quran50", name: "Hafiz in the making", d: "50 correct Qur'an answers", on: true },
  { id: "duel10", name: "Duelist", d: "Win 10 duels", on: false, prog: "6 / 10" },
  { id: "room", name: "Host", d: "Host a live room with 4 players", on: true },
  { id: "arabic", name: "Lisan", d: "Play every Arabic Language category", on: false, prog: "2 / 4" },
  { id: "streak30", name: "Month of light", d: "30-day streak", on: false, prog: "12 / 30" }
];

/* Interface strings. Core flow is translated; longer copy falls back to English
   (the app's own i18n files hold the full set). */
SJ.T = {
  home: { en: "Home", ar: "الرئيسية", fr: "Accueil" },
  play: { en: "Play", ar: "العب", fr: "Jouer" },
  challenges: { en: "Challenges", ar: "التحديات", fr: "Défis" },
  messages: { en: "Messages", ar: "الرسائل", fr: "Messages" },
  friends: { en: "Friends", ar: "الأصدقاء", fr: "Amis" },
  leaderboard: { en: "Leaderboard", ar: "المتصدّرون", fr: "Classement" },
  ranks: { en: "Ranks", ar: "الترتيب", fr: "Classement" },
  profile: { en: "My profile", ar: "ملفي الشخصي", fr: "Mon profil" },
  achievements: { en: "Achievements", ar: "الإنجازات", fr: "Succès" },
  settings: { en: "Settings", ar: "الإعدادات", fr: "Paramètres" },
  admin: { en: "Admin console", ar: "لوحة الإدارة", fr: "Console d'administration" },
  support: { en: "Contact us", ar: "تواصل معنا", fr: "Nous contacter" },
  signout: { en: "Sign out", ar: "تسجيل الخروج", fr: "Se déconnecter" },
  signin: { en: "Sign in", ar: "تسجيل الدخول", fr: "Se connecter" },
  signup: { en: "Create account", ar: "إنشاء حساب", fr: "Créer un compte" },
  guest: { en: "Play as a guest", ar: "العب كضيف", fr: "Jouer en invité" },
  light: { en: "Light", ar: "فاتح", fr: "Clair" },
  dark: { en: "Dark", ar: "داكن", fr: "Sombre" },
  device: { en: "Match my device", ar: "حسب إعداد جهازي", fr: "Comme mon appareil" },
  qotd: { en: "Question of the day", ar: "سؤال اليوم", fr: "Question du jour" },
  quick: { en: "Quick game", ar: "لعبة سريعة", fr: "Partie rapide" },
  start: { en: "Start game", ar: "ابدأ اللعبة", fr: "Lancer la partie" },
  question: { en: "Question", ar: "السؤال", fr: "Question" },
  of: { en: "of", ar: "من", fr: "sur" },
  next: { en: "Next question", ar: "السؤال التالي", fr: "Question suivante" },
  finish: { en: "See results", ar: "عرض النتيجة", fr: "Voir les résultats" },
  correct: { en: "Correct", ar: "إجابة صحيحة", fr: "Bonne réponse" },
  wrong: { en: "Not quite", ar: "إجابة خاطئة", fr: "Pas tout à fait" },
  timeup: { en: "Time's up", ar: "انتهى الوقت", fr: "Temps écoulé" },
  answerIs: { en: "The answer is", ar: "الإجابة الصحيحة:", fr: "La réponse est" },
  fifty: { en: "50 / 50", ar: "حذف إجابتين", fr: "50 / 50" },
  skip: { en: "Skip", ar: "تخطَّ", fr: "Passer" },
  quit: { en: "Quit game", ar: "إنهاء اللعبة", fr: "Quitter la partie" },
  source: { en: "Source", ar: "المصدر", fr: "Source" },
  points: { en: "points", ar: "نقطة", fr: "points" },
  results: { en: "Results", ar: "النتيجة", fr: "Résultats" },
  playAgain: { en: "Play again", ar: "العب مرة أخرى", fr: "Rejouer" },
  easy: { en: "Easy", ar: "سهل", fr: "Facile" },
  medium: { en: "Medium", ar: "متوسط", fr: "Moyen" },
  hard: { en: "Hard", ar: "صعب", fr: "Difficile" },
  mixed: { en: "Mixed", ar: "متنوع", fr: "Mélangé" },
  liveRoom: { en: "Live room", ar: "غرفة مباشرة", fr: "Salon en direct" },
  duel: { en: "1v1 duel", ar: "مبارزة فردية", fr: "Duel 1 contre 1" },
  join: { en: "Join with a code", ar: "انضم برمز", fr: "Rejoindre avec un code" },
  lobby: { en: "Lobby", ar: "غرفة الانتظار", fr: "Salle d'attente" },
  ready: { en: "Ready", ar: "جاهز", fr: "Prêt" },
  greeting: { en: "Assalamu alaikum", ar: "السلام عليكم", fr: "Assalamou alaykoum" }
};

SJ.NOTIFS = [
  { id: 1, kind: "challenge", icon: "swords", text: "Omar K. challenged you: Hadith, 10 questions", time: "8 min ago", unread: true, go: "challenges" },
  { id: 2, kind: "friend", icon: "users", text: "Nour E. sent you a friend request", time: "1 h ago", unread: true, go: "friends" },
  { id: 3, kind: "room", icon: "radio", text: "Khadija M. invited you to live room 7K4 Q2M", time: "2 h ago", unread: true, go: "room-join", code: "7K4Q2M" },
  { id: 4, kind: "support", icon: "mail", text: "Support replied to T-2086 “Please add more Hadith questions”", time: "Yesterday", unread: false, go: "messages" },
  { id: 5, kind: "achievement", icon: "star", text: "Achievement unlocked: Week of light", time: "2 days ago", unread: false, go: "profile" },
  { id: 6, kind: "rank", icon: "trophy", text: "You moved up to #18 this week", time: "3 days ago", unread: false, go: "leaderboard" },
  { id: 7, kind: "comment", icon: "chat", text: "Youssef A. replied to your comment on question #604", time: "Last week", unread: false, go: "history" }
];
SJ.HISTORY = [
  { mode: "solo", cat: "hadith", score: "7 / 10", pts: 142, when: "Today, 20:14", title: "Custom game" },
  { mode: "solo", cat: "quran", score: "9 / 10", pts: 188, when: "Today, 13:02", title: "Quick game" },
  { mode: "duel", cat: "fiqh", score: "Won 5–3", pts: 96, when: "Yesterday, 22:40", title: "Duel vs Youssef A." },
  { mode: "live", cat: "ramadan", score: "2nd of 5", pts: 120, when: "Yesterday, 21:10", title: "Live room 7K4 Q2M" },
  { mode: "device", cat: "prophets", score: "1st of 3", pts: 0, when: "Saturday", title: "On this device · family" },
  { mode: "challenge", cat: "seerah", score: "Lost 29–42", pts: 29, when: "Friday", title: "Challenge from Bilal R." },
  { mode: "solo", cat: "nahw", score: "3 / 5", pts: 46, when: "Thursday", title: "Arabic & Qur'anic Sciences" },
  { mode: "solo", cat: "prophets", score: "10 / 10", pts: 210, when: "Monday", title: "Custom game" }
];
SJ.SETS = [
  { id: "s1", name: "Family quiz night", lang: "en", plays: 3, updated: "2 days ago", qs: [
    { lang: "en", cat: "prophets", lvl: 1, c: 0, text: { en: { q: "Who built the Kaaba with his son?", a: ["Ibrahim", "Nuh", "Musa", "Adam"], x: "Ibrahim and his son Ismail raised its foundations (al-Baqarah 2:127)." }, ar: { q: "من بنى الكعبة مع ابنه؟", a: ["إبراهيم", "نوح", "موسى", "آدم"], x: "رفع إبراهيم وابنه إسماعيل قواعد البيت (البقرة 127)." } } },
    { lang: "en", cat: "ramadan", lvl: 1, c: 0, text: { en: { q: "How many days is the fast of Ramadan at most?", a: ["30", "29", "31", "28"], x: "A lunar month has 29 or 30 days." } } },
    { lang: "en", cat: "seerah", lvl: 1, c: 0, text: { en: { q: "Which city is the Prophet's ﷺ mosque in?", a: ["Madinah", "Makkah", "Jerusalem", "Taif"], x: "" } } },
    { lang: "en", cat: "quran", lvl: 2, c: 1, text: { en: { q: "Which surah is called the heart of the Qur'an?", a: ["Al-Fatihah", "Ya-Sin", "Al-Mulk", "Al-Kahf"], x: "" } } },
    { lang: "en", cat: "akhlaq", lvl: 1, c: 2, text: { en: { q: "What do we say when someone sneezes and praises Allah?", a: ["Bismillah", "SubhanAllah", "Yarhamuk Allah", "Allahu akbar"], x: "" } } },
    { lang: "en", cat: "history", lvl: 2, c: 3, text: { en: { q: "In which year did the Hijrah to Madinah take place?", a: ["610", "615", "630", "622"], x: "622 CE, the start of the Islamic calendar." } } }
  ] },
  { id: "s2", name: "Seerah for kids", lang: "en", plays: 0, updated: "Last week", qs: [
    { lang: "en", cat: "seerah", lvl: 1, c: 0, text: { en: { q: "What was the Prophet's ﷺ mother called?", a: ["Aminah", "Halimah", "Khadijah", "Fatimah"], x: "" } } },
    { lang: "en", cat: "seerah", lvl: 1, c: 1, text: { en: { q: "Who nursed the Prophet ﷺ in the desert?", a: ["Aminah", "Halimah", "Barakah", "Sawdah"], x: "" } } },
    { lang: "en", cat: "seerah", lvl: 2, c: 2, text: { en: { q: "How old was the Prophet ﷺ when revelation began?", a: ["25", "30", "40", "50"], x: "" } } }
  ] },
  { id: "s3", name: "مراجعة النحو", lang: "ar", plays: 0, updated: "Sep 28", qs: [
    { lang: "ar", cat: "nahw", lvl: 1, c: 0, text: { ar: { q: "ما إعراب «الطالبُ» في «نجحَ الطالبُ»؟", a: ["فاعل", "مفعول به", "مبتدأ", "خبر"], x: "" } } }
  ] }
];
SJ.COUNT = { quran: 8, seerah: 8, prophets: 8, fiqh: 8, hadith: 8, history: 8, ramadan: 8, akhlaq: 8, nahw: 6, vocab: 5, sarf: 4, balagha: 3 };
