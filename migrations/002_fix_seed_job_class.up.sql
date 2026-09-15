# $Id$
#
# Authors:
# 	Jeff Buchbinder <jeff@freemedsoftware.org>
#
# REMITT Electronic Medical Information Translation and Transmission
# Copyright (C) 1999- FreeMED Software Foundation
#
# This program is free software; you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation; either version 2 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program; if not, write to the Free Software
# Foundation, Inc., 675 Mass Ave, Cambridge, MA 02139, USA.

### Job Scheduler ###

# 001_legacy.up.sql seeded tJobs row 2 with the job class
# 'org.remitt.server.tasks.EligibiltyTask' - the second 'i' in "Eligibility"
# is missing. The Java task class
# (src/main/java/org/remitt/server/tasks/EligibilityTask.java) and the Go
# dispatch table (task/eligibility.go, EligibilityJobClass) both spell it
# 'org.remitt.server.tasks.EligibilityTask', so MasterControl/refreshJobs
# resolve the row to "Unknown job class" and the eligibility job never runs.
#
# 001_legacy.up.sql is not edited because existing installations have already
# executed it; the stored value is corrected here instead. This fixes every row
# carrying the misspelling, not only the seeded one.

UPDATE `tJobs`
   SET `jobClass` = 'org.remitt.server.tasks.EligibilityTask'
 WHERE `jobClass` = 'org.remitt.server.tasks.EligibiltyTask';
