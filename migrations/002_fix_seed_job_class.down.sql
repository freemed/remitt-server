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

# Reverse of 002_fix_seed_job_class.up.sql: restore the misspelled job class
# that 001_legacy.up.sql seeded, so the schema/data state matches the previous
# migration version again. It applies to the same set of rows the up migration
# rewrote - every tJobs row whose jobClass is the correctly spelled class name.

UPDATE `tJobs`
   SET `jobClass` = 'org.remitt.server.tasks.EligibiltyTask'
 WHERE `jobClass` = 'org.remitt.server.tasks.EligibilityTask';
