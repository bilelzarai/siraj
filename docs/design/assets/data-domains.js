/* Sirāj v2.1 — general-knowledge domains next to the Islamic flagship.
   family: "islamic" → keeps the geometric motif and shows in "Islamic content only" mode.
   Everything here is sample data; in the app it comes from the API. */
(function () {
  const SJ = window.SJ;

  /* Flagship + Islamic family */
  const isl = SJ.DOMAINS.find((d) => d.id === "islamic");
  Object.assign(isl, { family: "islamic", featured: true, color: "#0b7a5c", motif: "mosque" });
  const ar = SJ.DOMAINS.find((d) => d.id === "arabic");
  Object.assign(ar, { family: "islamic", color: "#a61e4d", motif: "feather", isNew: false });

  SJ.DOMAINS.push(
    { id: "sport", family: "general", isNew: true, icon: "ball", motif: "ball", color: "#c2410c",
      name: { en: "Sport", ar: "الرياضة", fr: "Sport" },
      desc: { en: "Football, Olympics and the games the world plays", ar: "كرة القدم والألعاب الأولمبية وألعاب العالم", fr: "Football, Jeux olympiques et sports du monde" } },
    { id: "geography", family: "general", isNew: true, icon: "map", motif: "globe", color: "#0b7285",
      name: { en: "Geography", ar: "الجغرافيا", fr: "Géographie" },
      desc: { en: "Capitals, rivers, deserts and maps", ar: "العواصم والأنهار والصحارى والخرائط", fr: "Capitales, fleuves, déserts et cartes" } },
    { id: "business", family: "general", isNew: true, icon: "briefcase", motif: "chart", color: "#364fc7",
      name: { en: "Business & Economy", ar: "الأعمال والاقتصاد", fr: "Business et économie" },
      desc: { en: "Money, markets and how companies work", ar: "المال والأسواق وكيف تعمل الشركات", fr: "Argent, marchés et entreprises" } },
    { id: "science", family: "general", isNew: true, icon: "atom", motif: "atom", color: "#862e9c",
      name: { en: "Science", ar: "العلوم", fr: "Sciences" },
      desc: { en: "Space, the human body and chemistry", ar: "الفضاء وجسم الإنسان والكيمياء", fr: "Espace, corps humain et chimie" } }
  );

  SJ.CATS.push(
    { id: "football", domain: "sport", icon: "ball", color: "#c2410c", name: { en: "Football", ar: "كرة القدم", fr: "Football" }, mastery: 41 },
    { id: "olympics", domain: "sport", icon: "trophy", color: "#b45309", name: { en: "Olympic Games", ar: "الألعاب الأولمبية", fr: "Jeux olympiques" }, mastery: 18 },
    { id: "worldsport", domain: "sport", icon: "target", color: "#be123c", name: { en: "World sports", ar: "رياضات العالم", fr: "Sports du monde" }, mastery: 9 },
    { id: "capitals", domain: "geography", icon: "pin", color: "#0b7285", name: { en: "Capitals", ar: "العواصم", fr: "Capitales" }, mastery: 56 },
    { id: "nature", domain: "geography", icon: "map", color: "#2b8a3e", name: { en: "Rivers, deserts & oceans", ar: "الأنهار والصحارى والمحيطات", fr: "Fleuves, déserts et océans" }, mastery: 33 },
    { id: "economics", domain: "business", icon: "chart", color: "#364fc7", name: { en: "Economics", ar: "الاقتصاد", fr: "Économie" }, mastery: 12 },
    { id: "money", domain: "business", icon: "coin", color: "#a16207", name: { en: "Money & currencies", ar: "المال والعملات", fr: "Argent et devises" }, mastery: 27 },
    { id: "companies", domain: "business", icon: "briefcase", color: "#1864ab", name: { en: "Companies & work", ar: "الشركات والعمل", fr: "Entreprises et travail" }, mastery: 0 },
    { id: "space", domain: "science", icon: "atom", color: "#862e9c", name: { en: "Space", ar: "الفضاء", fr: "Espace" }, mastery: 38 },
    { id: "body", domain: "science", icon: "heart", color: "#c92a2a", name: { en: "Human body", ar: "جسم الإنسان", fr: "Corps humain" }, mastery: 24 },
    { id: "chemistry", domain: "science", icon: "flask", color: "#5f3dc4", name: { en: "Chemistry", ar: "الكيمياء", fr: "Chimie" }, mastery: 6 }
  );

  const Q = (id, cat, lvl, c, src, q, a, x) => SJ.QUESTIONS.push({ id, cat, lvl, c, src, q, a, x });

  /* Sport */
  Q(1101, "football", 1, 0, "FIFA",
    { en: "Which country won the 2022 FIFA World Cup?", ar: "أي منتخب فاز بكأس العالم 2022؟", fr: "Quel pays a remporté la Coupe du monde 2022 ?" },
    { en: ["Argentina", "France", "Croatia", "Brazil"], ar: ["الأرجنتين", "فرنسا", "كرواتيا", "البرازيل"], fr: ["Argentine", "France", "Croatie", "Brésil"] },
    { en: "Argentina beat France on penalties in the final at Lusail Stadium.", ar: "فازت الأرجنتين على فرنسا بركلات الترجيح في النهائي على استاد لوسيل.", fr: "L'Argentine a battu la France aux tirs au but en finale, au stade de Lusail." });
  Q(1102, "football", 1, 2, "FIFA",
    { en: "Which country hosted the 2022 FIFA World Cup?", ar: "أي دولة استضافت كأس العالم 2022؟", fr: "Quel pays a accueilli la Coupe du monde 2022 ?" },
    { en: ["Saudi Arabia", "UAE", "Qatar", "Morocco"], ar: ["السعودية", "الإمارات", "قطر", "المغرب"], fr: ["Arabie saoudite", "Émirats arabes unis", "Qatar", "Maroc"] },
    { en: "Qatar hosted it, the first World Cup held in the Arab world.", ar: "استضافتها قطر، وهي أول كأس عالم تُقام في العالم العربي.", fr: "Le Qatar l'a accueillie : la première Coupe du monde dans le monde arabe." });
  Q(1103, "football", 2, 1, "FIFA",
    { en: "Which was the first African team to reach a World Cup semi-final?", ar: "ما أول منتخب إفريقي يبلغ نصف نهائي كأس العالم؟", fr: "Quelle fut la première équipe africaine en demi-finale d'une Coupe du monde ?" },
    { en: ["Senegal", "Morocco", "Cameroon", "Ghana"], ar: ["السنغال", "المغرب", "الكاميرون", "غانا"], fr: ["Sénégal", "Maroc", "Cameroun", "Ghana"] },
    { en: "Morocco reached the semi-finals in 2022 after beating Spain and Portugal.", ar: "بلغ المغرب نصف النهائي عام 2022 بعد فوزه على إسبانيا والبرتغال.", fr: "Le Maroc a atteint les demi-finales en 2022 après avoir battu l'Espagne et le Portugal." });
  Q(1111, "olympics", 1, 3, "International Olympic Committee",
    { en: "Where were the first modern Olympic Games held, in 1896?", ar: "أين أقيمت أول ألعاب أولمبية حديثة عام 1896؟", fr: "Où se sont tenus les premiers Jeux olympiques modernes, en 1896 ?" },
    { en: ["Paris", "London", "Rome", "Athens"], ar: ["باريس", "لندن", "روما", "أثينا"], fr: ["Paris", "Londres", "Rome", "Athènes"] },
    { en: "Athens hosted the first modern Games in 1896.", ar: "استضافت أثينا أول ألعاب حديثة عام 1896.", fr: "Athènes a accueilli les premiers Jeux modernes en 1896." });
  Q(1112, "olympics", 1, 1, "Olympic Charter",
    { en: "How many rings are on the Olympic flag?", ar: "كم عدد الحلقات في العلم الأولمبي؟", fr: "Combien d'anneaux compte le drapeau olympique ?" },
    { en: ["4", "5", "6", "7"], ar: ["4", "5", "6", "7"], fr: ["4", "5", "6", "7"] },
    { en: "Five interlocking rings, for the five inhabited continents.", ar: "خمس حلقات متشابكة ترمز إلى القارات المأهولة الخمس.", fr: "Cinq anneaux entrelacés, pour les cinq continents habités." });
  Q(1121, "worldsport", 1, 2, "FIBA rules",
    { en: "How many points is a basketball shot from beyond the arc worth?", ar: "كم نقطة تساوي رمية كرة السلة من خلف القوس؟", fr: "Combien vaut un tir au basket derrière l'arc ?" },
    { en: ["1", "2", "3", "4"], ar: ["1", "2", "3", "4"], fr: ["1", "2", "3", "4"] },
    { en: "A field goal from beyond the three-point line is worth 3.", ar: "الرمية من خلف خط الثلاث نقاط تُحتسب 3 نقاط.", fr: "Un panier derrière la ligne à trois points vaut 3 points." });
  Q(1122, "worldsport", 2, 0, "ITF rules of tennis",
    { en: "In tennis, what is a score of zero called?", ar: "في التنس، ماذا تُسمّى النتيجة صفر؟", fr: "Au tennis, comment appelle-t-on un score de zéro ?" },
    { en: ["Love", "Nil", "Deuce", "Ace"], ar: ["لاف (Love)", "نيل", "ديوس", "آس"], fr: ["Love (zéro)", "Nil", "Égalité", "Ace"] },
    { en: "Zero is called “love”. Deuce is 40–40 and an ace is an unreturned serve.", ar: "الصفر يُسمّى «لاف»، أما «ديوس» فهو التعادل 40–40، و«الآس» إرسال لا يُرد.", fr: "Zéro se dit « love » ; « égalité » correspond à 40–40 et un ace est un service gagnant." });

  /* Geography */
  Q(1201, "capitals", 1, 1, "UN country profiles",
    { en: "What is the capital of Australia?", ar: "ما عاصمة أستراليا؟", fr: "Quelle est la capitale de l'Australie ?" },
    { en: ["Sydney", "Canberra", "Melbourne", "Perth"], ar: ["سيدني", "كانبرا", "ملبورن", "بيرث"], fr: ["Sydney", "Canberra", "Melbourne", "Perth"] },
    { en: "Canberra was built as a compromise between Sydney and Melbourne.", ar: "بُنيت كانبرا حلًّا وسطًا بين سيدني وملبورن.", fr: "Canberra a été construite comme compromis entre Sydney et Melbourne." });
  Q(1202, "capitals", 1, 2, "UN country profiles",
    { en: "What is the capital of Canada?", ar: "ما عاصمة كندا؟", fr: "Quelle est la capitale du Canada ?" },
    { en: ["Toronto", "Montreal", "Ottawa", "Vancouver"], ar: ["تورونتو", "مونتريال", "أوتاوا", "فانكوفر"], fr: ["Toronto", "Montréal", "Ottawa", "Vancouver"] },
    { en: "Ottawa, in Ontario, is Canada's capital.", ar: "أوتاوا في مقاطعة أونتاريو هي عاصمة كندا.", fr: "Ottawa, en Ontario, est la capitale du Canada." });
  Q(1203, "capitals", 2, 0, "UN country profiles",
    { en: "What is the capital of Morocco?", ar: "ما عاصمة المغرب؟", fr: "Quelle est la capitale du Maroc ?" },
    { en: ["Rabat", "Casablanca", "Marrakesh", "Fez"], ar: ["الرباط", "الدار البيضاء", "مراكش", "فاس"], fr: ["Rabat", "Casablanca", "Marrakech", "Fès"] },
    { en: "Rabat is the capital; Casablanca is the largest city.", ar: "الرباط هي العاصمة، والدار البيضاء أكبر مدن المغرب.", fr: "Rabat est la capitale ; Casablanca est la plus grande ville." });
  Q(1211, "nature", 1, 0, "National Geographic",
    { en: "Which is the longest river in Africa?", ar: "ما أطول نهر في إفريقيا؟", fr: "Quel est le plus long fleuve d'Afrique ?" },
    { en: ["The Nile", "The Congo", "The Niger", "The Zambezi"], ar: ["النيل", "الكونغو", "النيجر", "الزمبيزي"], fr: ["Le Nil", "Le Congo", "Le Niger", "Le Zambèze"] },
    { en: "The Nile runs about 6,650 km to the Mediterranean.", ar: "يجري النيل نحو 6,650 كم حتى البحر المتوسط.", fr: "Le Nil parcourt environ 6 650 km jusqu'à la Méditerranée." });
  Q(1212, "nature", 1, 3, "NOAA",
    { en: "Which is the largest ocean?", ar: "ما أكبر محيط في العالم؟", fr: "Quel est le plus grand océan ?" },
    { en: ["Atlantic", "Indian", "Arctic", "Pacific"], ar: ["الأطلسي", "الهندي", "المتجمد الشمالي", "الهادئ"], fr: ["Atlantique", "Indien", "Arctique", "Pacifique"] },
    { en: "The Pacific covers about a third of the Earth's surface.", ar: "يغطي المحيط الهادئ نحو ثلث سطح الأرض.", fr: "Le Pacifique couvre environ un tiers de la surface de la Terre." });
  Q(1213, "nature", 2, 1, "National Geographic",
    { en: "Which is the largest hot desert in the world?", ar: "ما أكبر صحراء حارة في العالم؟", fr: "Quel est le plus grand désert chaud du monde ?" },
    { en: ["Arabian", "Sahara", "Gobi", "Kalahari"], ar: ["الصحراء العربية", "الصحراء الكبرى", "غوبي", "كالاهاري"], fr: ["Désert d'Arabie", "Sahara", "Gobi", "Kalahari"] },
    { en: "The Sahara spans North Africa, including southern Tunisia.", ar: "تمتد الصحراء الكبرى في شمال إفريقيا، ومنها جنوب تونس.", fr: "Le Sahara s'étend sur l'Afrique du Nord, dont le sud de la Tunisie." });

  /* Business */
  Q(1301, "economics", 1, 0, "IMF glossary",
    { en: "What does GDP stand for?", ar: "ماذا يعني الاختصار GDP؟", fr: "Que signifie le sigle PIB (GDP) ?" },
    { en: ["Gross Domestic Product", "General Debt Position", "Global Demand Price", "Gross Dividend Payout"], ar: ["الناتج المحلي الإجمالي", "وضع الدين العام", "سعر الطلب العالمي", "إجمالي توزيعات الأرباح"], fr: ["Produit intérieur brut", "Position de dette générale", "Prix de la demande mondiale", "Dividendes bruts versés"] },
    { en: "GDP is the total value of goods and services produced in a country.", ar: "الناتج المحلي الإجمالي هو قيمة كل السلع والخدمات المنتجة في البلد.", fr: "Le PIB mesure la valeur totale des biens et services produits dans un pays." });
  Q(1302, "economics", 1, 1, "IMF glossary",
    { en: "A general rise in prices over time is called…", ar: "يُسمّى الارتفاع العام في الأسعار مع الوقت…", fr: "Une hausse générale des prix dans le temps s'appelle…" },
    { en: ["Deflation", "Inflation", "Recession", "Devaluation"], ar: ["الانكماش", "التضخم", "الركود", "خفض قيمة العملة"], fr: ["Déflation", "Inflation", "Récession", "Dévaluation"] },
    { en: "Inflation means the same money buys less over time.", ar: "التضخم يعني أن المال نفسه يشتري أقل مع الوقت.", fr: "L'inflation signifie qu'une même somme achète moins avec le temps." });
  Q(1311, "money", 1, 2, "ISO 4217",
    { en: "What is the currency of Japan?", ar: "ما عملة اليابان؟", fr: "Quelle est la monnaie du Japon ?" },
    { en: ["Won", "Yuan", "Yen", "Baht"], ar: ["الوون", "اليوان", "الين", "البات"], fr: ["Won", "Yuan", "Yen", "Baht"] },
    { en: "The yen (JPY). The won is Korean, the yuan Chinese, the baht Thai.", ar: "الين الياباني. الوون كوري، واليوان صيني، والبات تايلندي.", fr: "Le yen (JPY). Le won est coréen, le yuan chinois, le baht thaïlandais." });
  Q(1312, "money", 1, 0, "ISO 4217",
    { en: "What is the currency of Qatar?", ar: "ما عملة قطر؟", fr: "Quelle est la monnaie du Qatar ?" },
    { en: ["Riyal", "Dinar", "Dirham", "Pound"], ar: ["الريال", "الدينار", "الدرهم", "الجنيه"], fr: ["Riyal", "Dinar", "Dirham", "Livre"] },
    { en: "The Qatari riyal (QAR).", ar: "الريال القطري (QAR).", fr: "Le riyal qatari (QAR)." });
  Q(1321, "companies", 1, 1, "Business dictionary",
    { en: "What does CEO stand for?", ar: "ماذا يعني الاختصار CEO؟", fr: "Que signifie CEO ?" },
    { en: ["Chief Economic Officer", "Chief Executive Officer", "Central Executive Office", "Company Equity Owner"], ar: ["المسؤول الاقتصادي الأول", "الرئيس التنفيذي", "المكتب التنفيذي المركزي", "مالك أسهم الشركة"], fr: ["Directeur économique", "Directeur général", "Bureau exécutif central", "Propriétaire des parts"] },
    { en: "The chief executive officer runs the company day to day.", ar: "الرئيس التنفيذي يدير الشركة يومًا بيوم.", fr: "Le directeur général dirige l'entreprise au quotidien." });
  Q(1322, "companies", 2, 3, "Accounting basics",
    { en: "A company's revenue minus all its costs is its…", ar: "إيرادات الشركة ناقص كل تكاليفها تساوي…", fr: "Le chiffre d'affaires d'une entreprise moins tous ses coûts, c'est son…" },
    { en: ["Turnover", "Cash flow", "Margin rate", "Net profit"], ar: ["رقم الأعمال", "التدفق النقدي", "نسبة الهامش", "صافي الربح"], fr: ["Chiffre d'affaires", "Flux de trésorerie", "Taux de marge", "Bénéfice net"] },
    { en: "What is left after every cost is paid is net profit.", ar: "ما يبقى بعد دفع كل التكاليف هو صافي الربح.", fr: "Ce qui reste une fois tous les coûts payés est le bénéfice net." });

  /* Science */
  Q(1401, "space", 1, 0, "NASA",
    { en: "Which planet is closest to the Sun?", ar: "ما أقرب كوكب إلى الشمس؟", fr: "Quelle planète est la plus proche du Soleil ?" },
    { en: ["Mercury", "Venus", "Mars", "Earth"], ar: ["عطارد", "الزهرة", "المريخ", "الأرض"], fr: ["Mercure", "Vénus", "Mars", "Terre"] },
    { en: "Mercury orbits closest, about 58 million km from the Sun.", ar: "عطارد هو الأقرب، على بعد نحو 58 مليون كم من الشمس.", fr: "Mercure est la plus proche, à environ 58 millions de km du Soleil." });
  Q(1402, "space", 1, 2, "NASA",
    { en: "Which is the largest planet in our solar system?", ar: "ما أكبر كوكب في مجموعتنا الشمسية؟", fr: "Quelle est la plus grande planète du système solaire ?" },
    { en: ["Saturn", "Neptune", "Jupiter", "Uranus"], ar: ["زحل", "نبتون", "المشتري", "أورانوس"], fr: ["Saturne", "Neptune", "Jupiter", "Uranus"] },
    { en: "Jupiter is more than twice as massive as all the other planets together.", ar: "كتلة المشتري أكبر من ضعف كتل الكواكب الأخرى مجتمعة.", fr: "Jupiter est plus de deux fois plus massive que toutes les autres planètes réunies." });
  Q(1411, "body", 2, 3, "Gray's Anatomy",
    { en: "How many bones does an adult human have?", ar: "كم عظمة في جسم الإنسان البالغ؟", fr: "Combien d'os compte un adulte ?" },
    { en: ["186", "196", "216", "206"], ar: ["186", "196", "216", "206"], fr: ["186", "196", "216", "206"] },
    { en: "206. Babies are born with around 270; some fuse as they grow.", ar: "206 عظمات. يولد الرضيع بنحو 270 عظمة يلتحم بعضها مع النمو.", fr: "206. Les bébés en ont environ 270 ; certains os fusionnent en grandissant." });
  Q(1412, "body", 2, 1, "NIH",
    { en: "Which organ makes insulin?", ar: "أي عضو يفرز الإنسولين؟", fr: "Quel organe produit l'insuline ?" },
    { en: ["Liver", "Pancreas", "Kidney", "Stomach"], ar: ["الكبد", "البنكرياس", "الكلية", "المعدة"], fr: ["Foie", "Pancréas", "Rein", "Estomac"] },
    { en: "Beta cells in the pancreas release insulin to control blood sugar.", ar: "تفرز خلايا بيتا في البنكرياس الإنسولين لضبط سكر الدم.", fr: "Les cellules bêta du pancréas libèrent l'insuline qui régule la glycémie." });
  Q(1421, "chemistry", 1, 2, "IUPAC periodic table",
    { en: "What is the chemical symbol for gold?", ar: "ما الرمز الكيميائي للذهب؟", fr: "Quel est le symbole chimique de l'or ?" },
    { en: ["Go", "Gd", "Au", "Ag"], ar: ["Go", "Gd", "Au", "Ag"], fr: ["Go", "Gd", "Au", "Ag"] },
    { en: "Au, from the Latin aurum. Ag is silver.", ar: "Au من اللاتينية aurum، أما Ag فهو الفضة.", fr: "Au, du latin aurum. Ag est l'argent." });

  Object.assign(SJ.COUNT, { football: 3, olympics: 2, worldsport: 2, capitals: 3, nature: 3, economics: 2, money: 2, companies: 2, space: 2, body: 2, chemistry: 1 });

  /* Question of the day rotates across domains; Islamic-only mode keeps it Islamic */
  SJ.QOTD_ALL = 1213; SJ.QOTD_ISLAMIC = 205; SJ.QOTD = SJ.QOTD_ALL;

  /* Neutral greeting by default; the Islamic greeting comes back in Islamic-only mode */
  SJ.T.greeting = { en: "Welcome back", ar: "أهلًا بعودتك", fr: "Bon retour" };
  SJ.T.greetingIslamic = { en: "Assalamu alaikum", ar: "السلام عليكم", fr: "Assalamou alaykoum" };
})();
