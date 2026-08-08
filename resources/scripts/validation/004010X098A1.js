// REMITT X12 4010 837P Validation
function validate() {
	var data = inputData;
	if (!data || data.length === 0) {
		return JSON.stringify({status: "error", messages: ["No input data"]});
	}

	var segments = data.split(/[~\n]/);

	var hasISA = false, hasGS = false, hasST = false;
	var hasSE = false, hasGE = false, hasIEA = false;

	for (var i = 0; i < segments.length; i++) {
		var seg = segments[i].trim();
		if (seg.indexOf("ISA") === 0) hasISA = true;
		if (seg.indexOf("GS") === 0)  hasGS  = true;
		if (seg.indexOf("ST") === 0)  hasST  = true;
		if (seg.indexOf("SE") === 0)  hasSE  = true;
		if (seg.indexOf("GE") === 0)  hasGE  = true;
		if (seg.indexOf("IEA") === 0) hasIEA = true;
	}

	var msgs = [];
	if (!hasISA) msgs.push("Missing ISA segment");
	if (!hasGS)  msgs.push("Missing GS segment");
	if (!hasST)  msgs.push("Missing ST segment");
	if (!hasSE)  msgs.push("Missing SE segment");
	if (!hasGE)  msgs.push("Missing GE segment");
	if (!hasIEA) msgs.push("Missing IEA segment");

	if (msgs.length > 0) {
		return JSON.stringify({status: "failure", messages: msgs});
	}

	return JSON.stringify({status: "success", messages: ["X12 structure valid"]});
}
