var editingGroupId = -1
function currentGroupId() { return editingGroupId }
var groups = []

// Save attempts to POST or PUT to /groups/
function save(id) {
    var targets = []
    $.each($("#targetsTable").DataTable().rows().data(), function (i, target) {
        targets.push({
            first_name: unescapeHtml(target[0]),
            last_name: unescapeHtml(target[1]),
            email: unescapeHtml(target[2]),
            position: unescapeHtml(target[3])
        })
    })
    var group = {
        name: $("#name").val(),
        targets: targets
    }
    // Submit the group
    if (id != -1) {
        // If we're just editing an existing group,
        // we need to PUT /groups/:id
        group.id = id
        api.groupId.put(group)
            .success(function (data) {
                successFlash("Group updated successfully!")
                load()
                dismiss()
                $("#modal").modal('hide')
            })
            .error(function (data) {
                modalError(data.responseJSON.message)
            })
    } else {
        // Else, if this is a new group, POST it
        // to /groups
        api.groups.post(group)
            .success(function (data) {
                successFlash("Group added successfully!")
                load()
                dismiss()
                $("#modal").modal('hide')
            })
            .error(function (data) {
                modalError(data.responseJSON.message)
            })
    }
}

// Directory sync UI. The panel appears only when the server has directory
// connectors configured and the group already exists (it needs an id).
var directoryConnectors = []

var hintForProvider = function (provider) {
    switch (provider) {
        case "msgraph": return "The Entra ID group's object ID."
        case "google": return "The Google Workspace group's email address."
        case "ldap": return "The group's distinguished name, e.g. CN=Sales,OU=Groups,DC=example,DC=com."
        default: return ""
    }
}

var updateDirectoryHint = function () {
    var conn = directoryConnectors.find(function (c) { return c.name === $("#directoryConnector").val() })
    $("#directoryExternalHint").text(conn ? hintForProvider(conn.provider) : "")
}

var renderSyncStatus = function (source) {
    if (!source) {
        $("#directorySyncStatus").text("")
        $("#removeSourceButton").addClass("hidden")
        return
    }
    $("#removeSourceButton").removeClass("hidden")
    var msg = source.last_synced_at ?
        "Last synced " + moment(source.last_synced_at).fromNow() : "Not synced yet"
    if (source.last_error) {
        msg += " — last error: " + source.last_error
    }
    $("#directorySyncStatus").text(msg)
}

var setupDirectorySync = function (id) {
    $("#directorySyncPanel").addClass("hidden")
    $("#directoryExternalId").val("")
    $("#directorySyncStatus").text("")
    $("#removeSourceButton").addClass("hidden")
    // New groups can't sync until they're saved and reopened.
    if (id === -1) {
        return
    }
    api.directory.connectors()
        .success(function (connectors) {
            if (!connectors || connectors.length === 0) {
                return
            }
            directoryConnectors = connectors
            var select = $("#directoryConnector").empty()
            $.each(connectors, function (i, c) {
                select.append($("<option>").val(c.name).text(c.name + " (" + c.provider + ")"))
            })
            $("#directorySyncPanel").removeClass("hidden")
            updateDirectoryHint()
            // Load an existing source, if any
            api.groupId.getSource(id)
                .success(function (source) {
                    $("#directoryConnector").val(source.connector)
                    $("#directoryExternalId").val(source.external_id)
                    updateDirectoryHint()
                    renderSyncStatus(source)
                })
                .error(function () { /* no source yet */ })
        })
        .error(function () { /* directory sync not configured */ })
}

function dismiss() {
    $("#targetsTable").dataTable().DataTable().clear().draw()
    $("#name").val("")
    $("#modal\\.flashes").empty()
}

function edit(id) {
    editingGroupId = id
    setupDirectorySync(id)
    targets = $("#targetsTable").dataTable({
        destroy: true, // Destroy any other instantiated table - http://datatables.net/manual/tech-notes/3#destroy
        columnDefs: [{
            orderable: false,
            targets: "no-sort"
        }]
    })
    $("#modalSubmit").unbind('click').click(function () {
        save(id)
    })
    if (id == -1) {
        $("#groupModalLabel").text("New Group");
        var group = {}
    } else {
        $("#groupModalLabel").text("Edit Group");
        api.groupId.get(id)
            .success(function (group) {
                $("#name").val(group.name)
                targetRows = []
                $.each(group.targets, function (i, record) {
                  targetRows.push([
                      escapeHtml(record.first_name),
                      escapeHtml(record.last_name),
                      escapeHtml(record.email),
                      escapeHtml(record.position),
                      '<span style="cursor:pointer;"><i class="fa fa-trash-o"></i></span>'
                  ])
                });
                targets.DataTable().rows.add(targetRows).draw()
            })
            .error(function () {
                errorFlash("Error fetching group")
            })
    }
    // Handle file uploads
    $("#csvupload").fileupload({
        url: "/api/import/group",
        dataType: "json",
        beforeSend: function (xhr) {
            xhr.setRequestHeader('X-CSRF-Token', csrf_token);
        },
        add: function (e, data) {
            $("#modal\\.flashes").empty()
            var acceptFileTypes = /(csv|txt)$/i;
            var filename = data.originalFiles[0]['name']
            if (filename && !acceptFileTypes.test(filename.split(".").pop())) {
                modalError("Unsupported file extension (use .csv or .txt)")
                return false;
            }
            data.submit();
        },
        done: function (e, data) {
            $.each(data.result, function (i, record) {
                addTarget(
                    record.first_name,
                    record.last_name,
                    record.email,
                    record.position);
            });
            targets.DataTable().draw();
        }
    })
}

var downloadCSVTemplate = function () {
    var csvScope = [{
        'First Name': 'Example',
        'Last Name': 'User',
        'Email': 'foobar@example.com',
        'Position': 'Systems Administrator'
    }]
    var filename = 'group_template.csv'
    var csvString = Papa.unparse(csvScope, {})
    var csvData = new Blob([csvString], {
        type: 'text/csv;charset=utf-8;'
    });
    if (navigator.msSaveBlob) {
        navigator.msSaveBlob(csvData, filename);
    } else {
        var csvURL = window.URL.createObjectURL(csvData);
        var dlLink = document.createElement('a');
        dlLink.href = csvURL;
        dlLink.setAttribute('download', filename)
        document.body.appendChild(dlLink)
        dlLink.click();
        document.body.removeChild(dlLink)
    }
}


var deleteGroup = function (id) {
    var group = groups.find(function (x) {
        return x.id === id
    })
    if (!group) {
        return
    }
    Swal.fire({
        title: "Are you sure?",
        text: "This will delete the group. This can't be undone!",
        type: "warning",
        animation: false,
        showCancelButton: true,
        confirmButtonText: "Delete " + escapeHtml(group.name),
        confirmButtonColor: "#428bca",
        reverseButtons: true,
        allowOutsideClick: false,
        preConfirm: function () {
            return new Promise(function (resolve, reject) {
                api.groupId.delete(id)
                    .success(function (msg) {
                        resolve()
                    })
                    .error(function (data) {
                        reject(data.responseJSON.message)
                    })
            })
        }
    }).then(function (result) {
        if (result.value){
            Swal.fire(
                'Group Deleted!',
                'This group has been deleted!',
                'success'
            );
        }
        $('button:contains("OK")').on('click', function () {
            location.reload()
        })
    })
}

function addTarget(firstNameInput, lastNameInput, emailInput, positionInput) {
    // Create new data row.
    var email = escapeHtml(emailInput).toLowerCase();
    var newRow = [
        escapeHtml(firstNameInput),
        escapeHtml(lastNameInput),
        email,
        escapeHtml(positionInput),
        '<span style="cursor:pointer;"><i class="fa fa-trash-o"></i></span>'
    ];

    // Check table to see if email already exists.
    var targetsTable = targets.DataTable();
    var existingRowIndex = targetsTable
        .column(2, {
            order: "index"
        }) // Email column has index of 2
        .data()
        .indexOf(email);
    // Update or add new row as necessary.
    if (existingRowIndex >= 0) {
        targetsTable
            .row(existingRowIndex, {
                order: "index"
            })
            .data(newRow);
    } else {
        targetsTable.row.add(newRow);
    }
}

function load() {
    $("#groupTable").hide()
    $("#emptyMessage").hide()
    $("#loading").show()
    api.groups.summary()
        .success(function (response) {
            $("#loading").hide()
            if (response.total > 0) {
                groups = response.groups
                $("#emptyMessage").hide()
                $("#groupTable").show()
                var groupTable = $("#groupTable").DataTable({
                    destroy: true,
                    columnDefs: [{
                        orderable: false,
                        targets: "no-sort"
                    }]
                });
                groupTable.clear();
                groupRows = []
                $.each(groups, function (i, group) {
                    groupRows.push([
                        escapeHtml(group.name),
                        escapeHtml(group.num_targets),
                        moment(group.modified_date).format('MMMM Do YYYY, h:mm:ss a'),
                        "<div class='pull-right'><button class='btn btn-primary' data-toggle='modal' data-backdrop='static' data-target='#modal' onclick='edit(" + group.id + ")'>\
                    <i class='fa fa-pencil'></i>\
                    </button>\
                    <button class='btn btn-danger' onclick='deleteGroup(" + group.id + ")'>\
                    <i class='fa fa-trash-o'></i>\
                    </button></div>"
                    ])
                })
                groupTable.rows.add(groupRows).draw()
            } else {
                $("#emptyMessage").show()
            }
        })
        .error(function () {
            errorFlash("Error fetching groups")
        })
}

// reloadTargetsFromGroup refreshes the targets table from a group object.
function reloadTargetsFromGroup(group) {
    var table = $("#targetsTable").DataTable()
    table.clear()
    var rows = []
    $.each(group.targets, function (i, record) {
        rows.push([
            escapeHtml(record.first_name),
            escapeHtml(record.last_name),
            escapeHtml(record.email),
            escapeHtml(record.position),
            '<span style="cursor:pointer;"><i class="fa fa-trash-o"></i></span>'
        ])
    })
    table.rows.add(rows).draw()
}

$(document).ready(function () {
    $("#directoryConnector").on("change", updateDirectoryHint)
    $("#saveSourceButton").on("click", function () {
        var id = currentGroupId()
        if (id === -1) { return }
        api.groupId.setSource(id, {
            connector: $("#directoryConnector").val(),
            external_id: $("#directoryExternalId").val()
        })
            .success(function (source) {
                successFlashFade("Directory source saved", 2)
                renderSyncStatus(source)
            })
            .error(function (data) { modalError(data.responseJSON.message) })
    })
    $("#removeSourceButton").on("click", function () {
        var id = currentGroupId()
        if (id === -1) { return }
        api.groupId.deleteSource(id)
            .success(function () {
                successFlashFade("Directory sync removed", 2)
                renderSyncStatus(null)
            })
            .error(function (data) { modalError(data.responseJSON.message) })
    })
    $("#syncNowButton").on("click", function () {
        var id = currentGroupId()
        if (id === -1) { return }
        var $btn = $(this).prop("disabled", true)
        api.groupId.sync(id)
            .success(function (group) {
                reloadTargetsFromGroup(group)
                api.groupId.getSource(id).success(renderSyncStatus)
                successFlash("Synced " + group.targets.length + " members from the directory")
            })
            .error(function (data) { modalError(data.responseJSON.message) })
            .always(function () { $btn.prop("disabled", false) })
    })
    load()
    // Setup the event listeners
    // Handle manual additions
    $("#targetForm").submit(function () {
        // Validate the form data
        var targetForm = document.getElementById("targetForm")
        if (!targetForm.checkValidity()) {
            targetForm.reportValidity()
            return
        }
        addTarget(
            $("#firstName").val(),
            $("#lastName").val(),
            $("#email").val(),
            $("#position").val());
        targets.DataTable().draw();

        // Reset user input.
        $("#targetForm>div>input").val('');
        $("#firstName").focus();
        return false;
    });
    // Handle Deletion
    $("#targetsTable").on("click", "span>i.fa-trash-o", function () {
        targets.DataTable()
            .row($(this).parents('tr'))
            .remove()
            .draw();
    });
    $("#modal").on("hide.bs.modal", function () {
        dismiss();
    });
    $("#csv-template").click(downloadCSVTemplate)
});
