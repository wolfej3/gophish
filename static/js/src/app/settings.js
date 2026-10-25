$(document).ready(function () {
    $('[data-toggle="tooltip"]').tooltip();
    $("#apiResetForm").submit(function (e) {
        api.reset()
            .success(function (response) {
                successFlash(response.message)
                $("#api_key").val(response.data)
            })
            .error(function (data) {
                errorFlash(data.message)
            })
        return false
    })
    // Revealing the full API key requires re-entering the password (and a
    // 2FA code if enabled), since it's no longer rendered into the page.
    $("#apiRevealButton").click(function (e) {
        e.preventDefault()
        $("#apiRevealPrompt").removeClass("hidden")
        $("#api_reveal_password").focus()
    })
    $("#apiRevealConfirm").click(function (e) {
        e.preventDefault()
        $.post("/settings/api_key/reveal", {
            current_password: $("#api_reveal_password").val(),
            code: $("#api_reveal_code").val(),
            csrf_token: csrf_token
        })
            .done(function (data) {
                $("#api_key").val(data.api_key)
                $("#apiRevealPrompt").addClass("hidden")
                $("#api_reveal_password, #api_reveal_code").val("")
            })
            .fail(function (data) {
                errorFlash(data.responseJSON.message)
            })
    })
    // Populate the default-team selector with the teams the user can use.
    api.teams.get()
        .success(function (teams) {
            teams = (teams || []).filter(function (t) {
                return !t.owner_user_id || (window.user && t.owner_user_id === window.user.id)
            })
            if (teams.length === 0) {
                return
            }
            var $select = $("#defaultTeamSelect").empty()
            var current = (window.user && window.user.default_team_id) || 0
            $.each(teams, function (i, t) {
                var label = (t.owner_user_id && window.user && t.owner_user_id === window.user.id) ? "Personal" : t.name
                $select.append($("<option>").val(t.id).text(label).prop("selected", t.id === current))
            })
            $("#defaultTeamGroup").show()
        })
    $("#defaultTeamSelect").change(function () {
        $.post("/settings/default_team", { default_team_id: $(this).val(), csrf_token: csrf_token })
            .done(function () { successFlash("Default team updated") })
            .fail(function (data) { errorFlash(data.responseJSON ? data.responseJSON.message : "Error updating default team") })
    })

    $("#settingsForm").submit(function (e) {
        $.post("/settings", $(this).serialize())
            .done(function (data) {
                successFlash(data.message)
            })
            .fail(function (data) {
                errorFlash(data.responseJSON.message)
            })
        return false
    })
    // Flash messages appear at the top of the page, well above the
    // two-factor section, so scroll up to show them.
    var totpFlash = function (flash, message) {
        flash(message)
        document.body.scrollTop = 0;
        document.documentElement.scrollTop = 0;
    }
    // The resources a limited API key can be given access to
    var apiKeyResources = {
        campaigns: "Campaigns",
        groups: "Users & Groups",
        templates: "Email Templates",
        pages: "Landing Pages",
        smtp: "Sending Profiles",
        import: "Importing emails and sites",
        util: "Sending test emails",
        imap: "Reporting (IMAP) settings",
        users: "User management",
        webhooks: "Webhooks",
        audit_log: "Audit log",
    }
    $.each(apiKeyResources, function (slug, name) {
        var id = "api_key_resource_" + slug
        $("#api_key_resources").append(
            $("<div class='checkbox checkbox-primary api-key-resource hidden'>")
                .append($("<input type='checkbox'>").attr("id", id).val(slug))
                .append($("<label>").attr("for", id).text(name))
        )
    })
    $("#api_key_resource_all").change(function () {
        $(".api-key-resource").toggleClass("hidden", this.checked)
    })
    var formatTime = function (t) {
        return t ? moment(t).format('MMMM Do YYYY, h:mm a') : "Never"
    }
    var loadAPIKeys = function () {
        api.apiKeys.get()
            .success(function (keys) {
                var body = $("#apiKeyTable tbody").empty()
                $("#apiKeyTable").toggleClass("hidden", keys.length == 0)
                $.each(keys, function (i, key) {
                    var resources = key.resources == "*" ? "All" :
                        key.resources.split(",").map(function (r) { return apiKeyResources[r] || r }).join(", ")
                    body.append($("<tr>")
                        .append($("<td>").text(key.name))
                        .append($("<td>").append($("<code>").text(key.prefix + "…")))
                        .append($("<td>").text(key.access == "write" ? "Read and write" : "Read only"))
                        .append($("<td>").text(resources))
                        .append($("<td>").text(key.last_used_at ? formatTime(key.last_used_at) : "Not yet"))
                        .append($("<td>").text(formatTime(key.expires_at)))
                        .append($("<td>").append(
                            $("<button type='button' class='btn btn-danger btn-sm api-key-delete'><i class='fa fa-trash-o'></i></button>")
                                .attr("data-key-id", key.id)
                        )))
                })
            })
            .error(function () {
                errorFlash("Error fetching API keys")
            })
    }
    $("#apiKeyForm").submit(function () {
        var resources = []
        if (!$("#api_key_resource_all").prop("checked")) {
            $(".api-key-resource input:checked").each(function () {
                resources.push($(this).val())
            })
            if (resources.length == 0) {
                errorFlash("Choose at least one resource for the key")
                return false
            }
        }
        api.apiKeys.post({
            name: $("#api_key_name").val(),
            access: $("#api_key_access").val(),
            resources: resources,
            expires_in_days: parseInt($("#api_key_expires").val(), 10),
        })
            .success(function (data) {
                $("#apiKeyCreatedValue").val(data.key)
                $("#apiKeyCreated").removeClass("hidden")
                $("#api_key_name").val("")
                loadAPIKeys()
            })
            .error(function (data) {
                errorFlash(data.responseJSON.message)
            })
        return false
    })
    $("#apiKeyTable").on("click", ".api-key-delete", function () {
        api.apiKeyId.delete($(this).attr("data-key-id"))
            .success(function (data) {
                successFlash(data.message)
                $("#apiKeyCreated").addClass("hidden")
                loadAPIKeys()
            })
            .error(function (data) {
                errorFlash(data.responseJSON.message)
            })
    })
    loadAPIKeys()
    var showRecoveryCodes = function (codes) {
        $("#totpRecoveryList").text(codes.join("\n"))
        $("#totpRecoveryCodes").removeClass("hidden")
    }
    $("#totpSetupForm").submit(function (e) {
        $.post("/settings/2fa/setup", $(this).serialize())
            .done(function (data) {
                $("#totpQRCode").attr("src", data.qr_code)
                $("#totpSecret").text(data.secret)
                $("#totpSetup").removeClass("hidden")
                $("#totp_enable_code").focus()
            })
            .fail(function (data) {
                totpFlash(errorFlash, data.responseJSON.message)
            })
        return false
    })
    $("#totpEnableForm").submit(function (e) {
        $.post("/settings/2fa/enable", $(this).serialize())
            .done(function (data) {
                successFlash(data.message)
                $("#totpDisabled").addClass("hidden")
                $("#totpSetup").addClass("hidden")
                $("#totpEnabled").removeClass("hidden")
                $("#totpSetupForm, #totpEnableForm").trigger("reset")
                showRecoveryCodes(data.recovery_codes)
            })
            .fail(function (data) {
                totpFlash(errorFlash, data.responseJSON.message)
            })
        return false
    })
    $("#totpRegenerate").click(function () {
        $.post("/settings/2fa/recovery_codes", $("#totpManageForm").serialize())
            .done(function (data) {
                successFlash(data.message)
                $("#totpManageForm").trigger("reset")
                showRecoveryCodes(data.recovery_codes)
            })
            .fail(function (data) {
                totpFlash(errorFlash, data.responseJSON.message)
            })
    })
    $("#totpDisable").click(function () {
        $.post("/settings/2fa/disable", $("#totpManageForm").serialize())
            .done(function (data) {
                totpFlash(successFlash, data.message)
                $("#totpManageForm").trigger("reset")
                $("#totpEnabled, #totpRecoveryCodes").addClass("hidden")
                $("#totpDisabled").removeClass("hidden")
            })
            .fail(function (data) {
                totpFlash(errorFlash, data.responseJSON.message)
            })
    })
    //$("#imapForm").submit(function (e) {
    $("#savesettings").click(function() {
        var imapSettings = {}
        imapSettings.host = $("#imaphost").val()
        imapSettings.port = $("#imapport").val()
        imapSettings.username = $("#imapusername").val()
        imapSettings.password = $("#imappassword").val()
        imapSettings.enabled = $('#use_imap').prop('checked')
        imapSettings.tls = $('#use_tls').prop('checked')

        //Advanced settings
        imapSettings.folder = $("#folder").val()
        imapSettings.imap_freq = $("#imapfreq").val()
        imapSettings.restrict_domain = $("#restrictdomain").val()
        imapSettings.ignore_cert_errors = $('#ignorecerterrors').prop('checked')
        imapSettings.delete_reported_campaign_email = $('#deletecampaign').prop('checked')
        
        //To avoid unmarshalling error in controllers/api/imap.go. It would fail gracefully, but with a generic error.
        if (imapSettings.host == ""){
            errorFlash("No IMAP Host specified")
            document.body.scrollTop = 0;
            document.documentElement.scrollTop = 0;
            return false
        }
        if (imapSettings.port == ""){
            errorFlash("No IMAP Port specified")
            document.body.scrollTop = 0;
            document.documentElement.scrollTop = 0;
            return false
        }
        if (isNaN(imapSettings.port) || imapSettings.port <1 || imapSettings.port > 65535  ){ 
            errorFlash("Invalid IMAP Port")
            document.body.scrollTop = 0;
            document.documentElement.scrollTop = 0;
            return false
        }
        if (imapSettings.imap_freq == ""){
            imapSettings.imap_freq = "60"
        }

        api.IMAP.post(imapSettings).done(function (data) {
                if (data.success == true) {
                    successFlashFade("Successfully updated IMAP settings.", 2)
                } else {
                    errorFlash("Unable to update IMAP settings.")
                }
            })
            .success(function (data){
                loadIMAPSettings()
            })
            .fail(function (data) {
                errorFlash(data.responseJSON.message)
            })
            .always(function (data){
                document.body.scrollTop = 0;
                document.documentElement.scrollTop = 0;
            })
        
        return false
    })

    $("#validateimap").click(function() {

        // Query validate imap server endpoint
        var server = {}
        server.host = $("#imaphost").val()
        server.port = $("#imapport").val()
        server.username = $("#imapusername").val()
        server.password = $("#imappassword").val()
        server.tls = $('#use_tls').prop('checked')
        server.ignore_cert_errors = $('#ignorecerterrors').prop('checked')

        //To avoid unmarshalling error in controllers/api/imap.go. It would fail gracefully, but with a generic error. 
        if (server.host == ""){
            errorFlash("No IMAP Host specified")
            document.body.scrollTop = 0;
            document.documentElement.scrollTop = 0;
            return false
        }
        if (server.port == ""){
            errorFlash("No IMAP Port specified")
            document.body.scrollTop = 0;
            document.documentElement.scrollTop = 0;
            return false
        }
        if (isNaN(server.port) || server.port <1 || server.port > 65535  ){
            errorFlash("Invalid IMAP Port")
            document.body.scrollTop = 0;
            document.documentElement.scrollTop = 0;
            return false
        }

        var oldHTML = $("#validateimap").html();
        // Disable inputs and change button text
        $("#imaphost").attr("disabled", true);
        $("#imapport").attr("disabled", true);
        $("#imapusername").attr("disabled", true);
        $("#imappassword").attr("disabled", true);
        $("#use_imap").attr("disabled", true);
        $("#use_tls").attr("disabled", true);
        $('#ignorecerterrors').attr("disabled", true);
        $("#folder").attr("disabled", true);
        $("#restrictdomain").attr("disabled", true);
        $('#deletecampaign').attr("disabled", true);
        $('#lastlogin').attr("disabled", true);
        $('#imapfreq').attr("disabled", true);
        $("#validateimap").attr("disabled", true);  
        $("#validateimap").html("<i class='fa fa-circle-o-notch fa-spin'></i> Testing...");
        
        api.IMAP.validate(server).done(function(data) {
            if (data.success == true) {
                Swal.fire({
                    title: "Success",
                    html: "Logged into <b>" + escapeHtml($("#imaphost").val()) + "</b>",
                    type: "success",
                })
            } else {
                Swal.fire({
                    title: "Failed!",
                    html: "Unable to login to <b>" + escapeHtml($("#imaphost").val()) + "</b>.",
                    type: "error",
                    showCancelButton: true,
                    cancelButtonText: "Close",
                    confirmButtonText: "More Info",
                    confirmButtonColor: "#428bca",
                    allowOutsideClick: false,
                }).then(function(result) {
                    if (result.value) {
                        Swal.fire({
                            title: "Error:",
                            text: data.message,
                        })
                    }
                  })
            }
            
          })
          .fail(function() {
            Swal.fire({
                title: "Failed!",
                text: "An unecpected error occured.",
                type: "error",
            })
          })
          .always(function() {
            //Re-enable inputs and change button text
            $("#imaphost").attr("disabled", false);
            $("#imapport").attr("disabled", false);
            $("#imapusername").attr("disabled", false);
            $("#imappassword").attr("disabled", false);
            $("#use_imap").attr("disabled", false);
            $("#use_tls").attr("disabled", false);
            $('#ignorecerterrors').attr("disabled", false);
            $("#folder").attr("disabled", false);
            $("#restrictdomain").attr("disabled", false);
            $('#deletecampaign').attr("disabled", false);
            $('#lastlogin').attr("disabled", false);
            $('#imapfreq').attr("disabled", false);
            $("#validateimap").attr("disabled", false);
            $("#validateimap").html(oldHTML);

          });

      }); //end testclick

    $("#reporttab").click(function() {
        loadIMAPSettings()
    })

    $("#advanced").click(function() {
        $("#advancedarea").toggle();
    })

    function loadIMAPSettings(){
        api.IMAP.get()
        .success(function (imap) {
            if (imap.length == 0){
                $('#lastlogindiv').hide()
            } else {
                imap = imap[0]
                if (imap.enabled == false){
                    $('#lastlogindiv').hide()
                } else {
                    $('#lastlogindiv').show()
                }
                $("#imapusername").val(imap.username)
                $("#imaphost").val(imap.host)
                $("#imapport").val(imap.port)
                $("#imappassword").val(imap.password)
                $('#use_tls').prop('checked', imap.tls)
                $('#ignorecerterrors').prop('checked', imap.ignore_cert_errors)
                $('#use_imap').prop('checked', imap.enabled)
                $("#folder").val(imap.folder)
                $("#restrictdomain").val(imap.restrict_domain)
                $('#deletecampaign').prop('checked', imap.delete_reported_campaign_email)
                $('#lastloginraw').val(imap.last_login)
                $('#lastlogin').val(moment.utc(imap.last_login).fromNow())
                $('#imapfreq').val(imap.imap_freq)
            }  

        })
        .error(function () {
            errorFlash("Error fetching IMAP settings")
        })
    }

    var use_map = localStorage.getItem('gophish.use_map')
    $("#use_map").prop('checked', JSON.parse(use_map))
    $("#use_map").on('change', function () {
        localStorage.setItem('gophish.use_map', JSON.stringify(this.checked))
    })

    loadIMAPSettings()
})
