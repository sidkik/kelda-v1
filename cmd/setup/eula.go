package setup

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sidkik/kelda-v1/cmd/util"
	"github.com/sidkik/kelda-v1/pkg/errors"
)

// EULA is the text of the EULA shown to free trial users
const EULA = `Software End User License Agreement

This Software End User License Agreement ("Agreement") is between you
(both the individual installing the Program and any single legal
entity on behalf of which such individual is acting) ("You" or "Your")
and Kelda Inc. ("Kelda") regarding Your use of Kelda’s software, in
object code form only (the "Program").

IT IS IMPORTANT THAT YOU READ CAREFULLY AND UNDERSTAND THIS AGREEMENT.
BY TYPING "Y" AT THE SUBSEQENT PROMPT, YOU AGREE TO BE BOUND BY THIS
AGREEMENT.  IF YOU DO NOT AGREE WITH ALL THE TERMS OF THIS AGREEMENT
AND DO NOT AGREE TO BE BOUND BY THIS AGREEMENT, PLEASE TYPE "N" AT THE
PROMPT.  IF YOU DO NOT ACCEPT THIS AGREEMENT, YOU WILL NOT BE REGISTERED
TO USE OR ACCESS THE PROGRAM.

1. Program license

   1. Limited License.  Kelda hereby grants to You a limited,
   non-exclusive, non-transferable license (without the right to
   sublicense): (a) to use a single copy of the Program solely for
   Your own internal business operations; and (b) to copy the Program
   for archival or backup purposes, provided that all titles and
   trademark, copyright and restricted rights notices are reproduced
   on all such copies.

   2. Restrictions.  You will not copy or use the Program except as
   expressly permitted by this Agreement.  You will not relicense,
   sublicense, rent or lease the Program or use the Program for
   third-party training, commercial time-sharing or service bureau
   use.  You will not, and will not permit any third party to, reverse
   engineer, disassemble or decompile any Program, except to the
   extent expressly permitted by applicable law, and then only after
   You have notified Kelda in writing of Your intended activities.

   3. Ownership.  Kelda will retain all right, title and interest in
   and to the patent, copyright, trademark, trade secret and any other
   intellectual property rights in the Program and any derivative
   works thereof, subject only to the limited licenses set forth in
   this Agreement.  You do not acquire any other rights, express or
   implied, in the Program other than those rights expressly granted
   under this Agreement.

   4. No Support.  Kelda has no obligation to provide support,
   maintenance, upgrades, modifications or new releases under this
   Agreement.

   5. Analytics.  You acknowledge that the Program collects analytics
   and statistics arising from Your use of the Program and You hereby
   consents to the Program’s transfer of such analytics and statistics
   to Kelda and Kelda shall have the right to use such analytics and
   statistics.

2. DISCLAIMER KELDA MAKES NO REPRESENTATIONS OR WARRANTIES, EITHER
   EXPRESS OR IMPLIED, OF ANY KIND WITH RESPECT TO THE PROGRAM.  THE
   PROGRAM IS PROVIDED “AS IS” WITH NO WARRANTY.  YOU AGREE THAT YOUR
   USE OF THE PROGRAM IS AT YOUR SOLE RISK.  TO THE FULLEST EXTENT
   PERMISSIBLE UNDER APPLICABLE LAW, KELDA EXPRESSLY DISCLAIMS ALL
   WARRANTIES OF ANY KIND, EXPRESS OR IMPLIED, WITH RESPECT TO THE
   PROGRAM, INCLUDING WARRANTIES OF MERCHANTABILITY, FITNESS FOR A
   PARTICULAR PURPOSE, SATISFACTORY QUALITY, ACCURACY, TITLE AND
   NON-INFRINGEMENT, AND ANY WARRANTIES THAT MAY ARISE OUT OF COURSE
   OF PERFORMANCE, COURSE OF DEALING OR USAGE OF TRADE.  Kelda does
   not warrant that the Program will operate in combination with
   hardware, software, systems or data not provided by Kelda, or that
   the operation of the Program will be uninterrupted or error-free.

3. Termination This Agreement is effective for a period of thirty
   (30) days after Kelda provides You the Program.  Kelda may
   terminate this Agreement at any time upon Your breach of any of the
   provisions hereof.  Upon termination of this Agreement, You will
   cease all use of the Program, return to Kelda or destroy the
   Program and all related materials in Your possession, and so
   certify to Kelda.  Except for the license granted herein and as
   expressly provided herein, the terms of this Agreement will survive
   termination.

4. General Terms

   4.1. Law.  This Agreement and all matters arising out of or
   relating to this Agreement will be governed by the internal laws of
   the State of California without giving effect to any choice of law
   rule.  This Agreement will not be governed by the United Nations
   Convention on Contracts for the International Sales of Goods, the
   application of which is expressly excluded.  In the event of any
   controversy, claim or dispute between the parties arising out of or
   relating to this Agreement, such controversy, claim or dispute may
   be tried solely in a state or federal court for Santa Clara County,
   California, and the parties hereby irrevocably consent to the
   jurisdiction and venue of such courts.

   4.2. Limitation of Liability.  In no event will either party be
   liable for any indirect, incidental, special, consequential or
   punitive damages, or damages for loss of profits, revenue,
   business, savings, data, use or cost of substitute procurement,
   incurred by either party or any third party, whether in an action
   in contract or tort, even if the other party has been advised of
   the possibility of such damages or if such damages are foreseeable.
   In no event will Kelda’s liability for damages hereunder exceed the
   amounts actually paid by You to Kelda for the Program.  The parties
   acknowledge that the limitations of liability in this Section 4.2
   and in the other provisions of this Agreement and the allocation of
   risk herein are an essential element of the bargain between the
   parties, without which Kelda would not have entered into this
   Agreement.  Kelda’s pricing reflects this allocation of risk and
   the limitation of liability specified herein.

   4.3. Severability and Waiver.  If any provision of this Agreement
   is held to be illegal, invalid or otherwise unenforceable, such
   provision will be enforced to the extent possible consistent with
   the stated intention of the parties, or, if incapable of such
   enforcement, will be deemed to be severed and deleted from this
   Agreement, while the remainder of this Agreement will continue in
   full force and effect.  The waiver by either party of any default
   or breach of this Agreement will not constitute a waiver of any
   other or subsequent default or breach.

   4.4. No Assignment. You may not assign, sell, transfer, delegate
   or otherwise dispose of, whether voluntarily or involuntarily, by
   operation of law or otherwise, this Agreement or any rights or
   obligations under this Agreement without the prior written consent
   of Kelda.  Any purported assignment, transfer or delegation by You
   will be null and void.  Subject to the foregoing, this Agreement
   will be binding upon and will inure to the benefit of the parties
   and their respective successors and assigns.

   4.5. Export Administration.  You will comply fully with all
   relevant export laws and regulations of the United States,
   including, without limitation, the U.S. Export Administration
   Regulations (collectively “Export Controls”).  Without limiting the
   generality of the foregoing, You will not, and You will require
   Your representatives not to, export, direct or transfer the
   Program, or any direct product thereof, to any destination, person
   or entity restricted or prohibited by the Export Controls.

   4.6. Entire Agreement.  This Agreement constitutes the entire
   agreement between the parties and, other than any Kelda standard
   form customer agreement signed by the parties, supersedes all prior
   or contemporaneous agreements or representations, written or oral,
   concerning the subject matter of this Agreement.  In the event of a
   conflict between the terms of this Agreement and a signed Kelda
   standard form customer Agreement, the terms of the signed customer
   agreement will control.  This Agreement may not be modified or
   amended except in a writing signed by a duly authorized
   representative of each party; no other act, document, usage or
   custom will be deemed to amend or modify this Agreement.  It is
   expressly agreed that the terms of this Agreement will supersede
   the terms in any of Your purchase orders or other ordering
   documents.


BY TYPING "Y" AT THE PROMPT, YOU ACKNOWLEDGE THAT (1) YOU HAVE READ AND
REVIEWED THIS AGREEMENT IN ITS ENTIRETY, (2) YOU AGREE TO BE BOUND BY
THIS AGREEMENT, (3) THE INDIVIDUAL SO TYPING HAS THE POWER, AUTHORITY
AND LEGAL RIGHT TO ENTER INTO THIS AGREEMENT ON BEHALF OF YOU AND, (4)
BY SO TYPING, THIS AGREEMENT CONSTITUTES BINDING AND ENFORCEABLE
OBLIGATIONS OF YOU.`

// ShowEULA shows the EULA and collects whether the user agreed or not.
func ShowEULA() (bool, error) {
	fmt.Println("You will now be shown the End User License Agreement. " +
		"Once you have read it, press 'q' to close the pager.\nPlease press " +
		"enter to continue.")
	var reply string
	fmt.Scanln(&reply)

	path, err := exec.LookPath("less")
	if err != nil {
		return false, errors.WithContext(err, "find less")
	}
	cmd := exec.Command(path)
	cmd.Stdin = strings.NewReader(EULA)
	cmd.Stdout = os.Stdout

	err = cmd.Run()
	if err != nil {
		return false, errors.WithContext(err, "run less")
	}

	accept, err := util.PromptYesOrNo("Do you accept the EULA?")
	if err != nil {
		return false, errors.WithContext(err, "get eula reply")
	}

	return accept, nil
}
