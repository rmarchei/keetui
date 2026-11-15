package keepass

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// dicewareWordList is a curated list of 1050 four-letter words for
// generating passphrases (~10.0 bits of entropy per word, so keetui's
// default 8-word passphrase carries ~80 bits). About a third of the words
// come from the EFF Short Wordlist #1 (https://www.eff.org/dice), which the
// Electronic Frontier Foundation publishes under CC BY 3.0 US; the rest are
// keetui's own.
var dicewareWordList = []string{
	"acid", "acme", "acre", "aged", "also", "arch", "area", "army",
	"atom", "aunt", "away", "axis", "back", "bail", "bake", "ball",
	"band", "bank", "barn", "base", "bath", "bead", "beam", "bean",
	"bear", "beat", "been", "beer", "bell", "belt", "bend", "bent",
	"best", "bike", "bind", "bird", "bite", "blow", "blue", "blur",
	"boat", "body", "bold", "bolt", "bomb", "bond", "bone", "book",
	"boot", "bore", "born", "boss", "both", "bowl", "bulk", "bull",
	"burn", "bush", "busy", "buzz", "cafe", "cage", "cake", "call",
	"calm", "came", "camp", "cape", "card", "care", "cart", "case",
	"cash", "cast", "cave", "chef", "chin", "chip", "chop", "cite",
	"city", "clad", "clan", "clap", "clay", "clip", "club", "clue",
	"coal", "coat", "code", "coil", "coin", "cold", "cole", "colt",
	"come", "cook", "cool", "cope", "copy", "cord", "core", "corn",
	"cost", "cozy", "crew", "crop", "crow", "cube", "cult", "cups",
	"curb", "cure", "curl", "cute", "dado", "dale", "dame", "damp",
	"dare", "dark", "dart", "dash", "data", "date", "dawn", "days",
	"dead", "deaf", "deal", "dean", "dear", "debt", "deck", "deed",
	"deem", "deep", "deer", "demo", "dent", "deny", "desk", "dial",
	"dice", "died", "diet", "dime", "dine", "dire", "dirt", "disc",
	"dish", "disk", "dock", "does", "doll", "dome", "done", "doom",
	"door", "dose", "dove", "down", "draw", "drew", "drip", "drop",
	"drum", "dual", "duck", "dude", "duel", "duet", "duke", "dull",
	"duly", "dump", "dune", "dusk", "dust", "duty", "dyed", "each",
	"earl", "earn", "ease", "east", "easy", "edge", "edit", "else",
	"emit", "ends", "epic", "even", "ever", "evil", "exam", "exit",
	"eyed", "eyes", "face", "fact", "fade", "fail", "fair", "fake",
	"fall", "fame", "fang", "fare", "farm", "fast", "fate", "fawn",
	"fear", "feat", "feed", "feel", "feet", "fell", "felt", "fend",
	"fern", "fest", "feud", "file", "fill", "film", "find", "fine",
	"fire", "firm", "fish", "fist", "five", "flag", "flap", "flat",
	"flaw", "fled", "flew", "flex", "flip", "flog", "flow", "foam",
	"foil", "fold", "folk", "fond", "font", "food", "fool", "foot",
	"ford", "fore", "fork", "form", "fort", "foul", "four", "fowl",
	"free", "frog", "from", "fuel", "full", "fund", "fury", "fuse",
	"fuss", "gain", "gait", "gale", "game", "gang", "gape", "garb",
	"gate", "gave", "gaze", "gear", "gene", "gift", "gild", "gilt",
	"girl", "give", "glad", "glen", "glow", "glue", "goat", "goes",
	"gold", "golf", "gone", "good", "gore", "grab", "gram", "gray",
	"grew", "grid", "grim", "grin", "grip", "grit", "grow", "gulf",
	"guru", "gust", "guts", "hack", "hail", "hair", "hale", "half",
	"hall", "halt", "hand", "hang", "hare", "harm", "harp", "hash",
	"hate", "haul", "have", "hawk", "haze", "hazy", "head", "heal",
	"heap", "hear", "heat", "heed", "heel", "held", "helm", "help",
	"herb", "herd", "here", "hero", "hide", "high", "hike", "hill",
	"hint", "hire", "hiss", "hive", "hold", "hole", "holy", "home",
	"hood", "hook", "hope", "horn", "hose", "host", "hour", "hubs",
	"huge", "hull", "hung", "hunt", "hurl", "hurt", "husk", "hymn",
	"icon", "idea", "idle", "inch", "info", "into", "iron", "isle",
	"item", "jack", "jade", "jail", "jazz", "jean", "jerk", "jest",
	"jobs", "jock", "join", "joke", "jolt", "jury", "just", "keen",
	"keep", "kept", "kick", "kids", "kill", "kind", "king", "kiss",
	"kite", "knit", "knob", "knot", "know", "lace", "lack", "lady",
	"laid", "lake", "lamb", "lame", "lamp", "land", "lane", "laps",
	"lark", "lash", "lass", "last", "late", "lawn", "lead", "leaf",
	"leak", "lean", "leap", "left", "lend", "lens", "lent", "less",
	"lick", "lied", "lieu", "life", "lift", "like", "limb", "lime",
	"limp", "line", "link", "lion", "list", "live", "load", "loaf",
	"loan", "lock", "loft", "logo", "lone", "long", "look", "loop",
	"lord", "lore", "lose", "loss", "lost", "lots", "loud", "love",
	"luck", "lump", "lung", "lure", "lurk", "lush", "lust", "mace",
	"made", "maid", "mail", "main", "make", "male", "mall", "malt",
	"mane", "many", "maps", "mare", "mark", "mart", "mash", "mask",
	"mass", "mast", "mate", "maze", "mead", "meal", "mean", "meat",
	"meet", "meld", "melt", "memo", "mend", "menu", "mere", "mesh",
	"mice", "mild", "mile", "milk", "mill", "mime", "mind", "mine",
	"mint", "miss", "mist", "moan", "moat", "mock", "mode", "mold",
	"monk", "mood", "moon", "moor", "more", "moss", "most", "moth",
	"move", "much", "mule", "mull", "muse", "mush", "must", "mute",
	"myth", "nail", "name", "navy", "near", "neat", "neck", "need",
	"nest", "nets", "news", "next", "nice", "nick", "nine", "node",
	"none", "noon", "norm", "nose", "note", "noun", "null", "numb",
	"nuts", "oaks", "oath", "obey", "odds", "odor", "oils", "okay",
	"omen", "omit", "once", "ones", "only", "onto", "opal", "open",
	"opts", "oral", "orca", "ours", "oust", "oven", "over", "owed",
	"owes", "owls", "owns", "pace", "pack", "pact", "page", "paid",
	"pail", "pain", "pair", "pale", "palm", "pane", "pang", "park",
	"part", "pass", "past", "path", "pave", "pawn", "peak", "peal",
	"pear", "peat", "peck", "peel", "peer", "pelt", "pens", "perk",
	"pest", "pick", "pier", "pike", "pile", "pill", "pine", "pink",
	"pins", "pint", "pipe", "pity", "plan", "play", "plea", "plod",
	"plot", "plow", "ploy", "plug", "plum", "plus", "pods", "poem",
	"poet", "poke", "pole", "poll", "polo", "pond", "pony", "pool",
	"poor", "pope", "pops", "pork", "port", "pose", "post", "pour",
	"pray", "prey", "prod", "prop", "prow", "pubs", "pull", "pulp",
	"pump", "punk", "pure", "push", "quiz", "race", "rack", "raft",
	"rage", "raid", "rail", "rain", "rake", "ramp", "rang", "rank",
	"rare", "rash", "rate", "rave", "rays", "read", "real", "reap",
	"rear", "reed", "reef", "reel", "rely", "rent", "rest", "rice",
	"rich", "ride", "rift", "ring", "riot", "ripe", "rise", "risk",
	"road", "roam", "robe", "rock", "rode", "role", "roll", "roof",
	"room", "root", "rope", "rose", "rosy", "rout", "rove", "rows",
	"rude", "ruin", "rule", "rump", "rung", "rush", "rust", "sack",
	"safe", "saga", "sage", "said", "sail", "sake", "sale", "salt",
	"same", "sand", "sane", "sang", "sank", "sash", "save", "seal",
	"seam", "seat", "sect", "seed", "seek", "seem", "seen", "self",
	"sell", "send", "sent", "sept", "shed", "shin", "ship", "shop",
	"shot", "show", "shut", "sick", "side", "sift", "sigh", "sign",
	"silk", "sill", "silt", "sing", "sink", "sips", "site", "sits",
	"size", "skim", "skin", "skip", "slab", "slam", "slap", "slew",
	"slid", "slim", "slip", "slit", "slot", "slow", "slug", "slum",
	"smog", "snap", "snip", "snow", "snub", "snug", "soak", "soap",
	"soar", "sock", "soda", "sofa", "soft", "soil", "sold", "sole",
	"some", "song", "soon", "soot", "sore", "sort", "soul", "sour",
	"sown", "span", "spar", "spec", "sped", "spin", "spit", "spot",
	"spur", "stab", "stag", "star", "stay", "stem", "step", "stew",
	"stir", "stop", "stow", "stub", "stud", "stun", "such", "suit",
	"sulk", "sung", "sunk", "sure", "surf", "swan", "swap", "sway",
	"swim", "tabs", "tack", "tact", "tags", "tail", "take", "tale",
	"talk", "tall", "tame", "tang", "tank", "tape", "taps", "tarn",
	"task", "taxi", "teak", "teal", "team", "tear", "teen", "tell",
	"temp", "tend", "tent", "term", "test", "text", "than", "that",
	"them", "then", "they", "thin", "this", "thus", "tick", "tide",
	"tidy", "tied", "tier", "tile", "till", "tilt", "time", "tint",
	"tiny", "tips", "tire", "toad", "toes", "toil", "told", "toll",
	"tomb", "tone", "took", "tool", "tops", "tore", "torn", "toss",
	"tour", "town", "toys", "trap", "tray", "tree", "trek", "trim",
	"trio", "trip", "trod", "trot", "true", "tube", "tuck", "tuft",
	"tuna", "tune", "turf", "turn", "tusk", "twin", "type", "unit",
	"unto", "upon", "urge", "used", "user", "uses", "vain", "vale",
	"vane", "vary", "vase", "vast", "veil", "vein", "vent", "verb",
	"very", "vest", "veto", "vial", "vice", "view", "vine", "visa",
	"void", "volt", "vote", "vows", "wade", "wage", "wail", "wait",
	"wake", "walk", "wall", "wand", "want", "ward", "warm", "warn",
	"warp", "wary", "wash", "wave", "wavy", "waxy", "ways", "weak",
	"wean", "wear", "weed", "week", "weep", "weld", "well", "went",
	"were", "west", "what", "when", "whim", "whom", "wick", "wide",
	"wife", "wild", "will", "wilt", "wily", "wind", "wine",
	"wing", "wink", "wipe", "wire", "wise", "wish", "wisp", "with",
	"wits", "woke", "wolf", "wood", "wool", "word", "wore", "work",
	"worm", "worn", "wove", "wrap", "wren", "writ", "yank", "yard",
	"yarn", "year", "yell", "yoga", "yoke", "your", "zeal", "zero",
	"zinc", "zone", "zoom",
}

// GeneratePassphrase creates a diceware-style passphrase with the given
// number of words (4 to 12), separated by the given separator.
func GeneratePassphrase(wordCount int, separator string) (string, error) {
	wordCount = min(max(wordCount, 4), 12)

	listLen := big.NewInt(int64(len(dicewareWordList)))
	words := make([]string, wordCount)
	for i := range words {
		n, err := rand.Int(rand.Reader, listLen)
		if err != nil {
			return "", fmt.Errorf("random word selection: %w", err)
		}
		words[i] = dicewareWordList[n.Int64()]
	}

	return strings.Join(words, separator), nil
}
