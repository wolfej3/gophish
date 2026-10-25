let schedules = [];
let groups = [];

const dateFormat = "MMMM Do YYYY, h:mm a";

// localZone returns the operator's browser time zone, which is the zone the
// entered launch time is interpreted in before being stored as UTC.
const localZone = () => {
    try {
        return Intl.DateTimeFormat().resolvedOptions().timeZone;
    } catch (e) {
        return "your local time";
    }
};

const dismiss = () => {
    $("#name").val("");
    $("#url").val("");
    $("#send_by_offset").val("");
    $("#repeat_interval").val("weekly");
    $("#enabled").prop("checked", true);
    $("#template").val("").trigger("change");
    $("#page").val("").trigger("change");
    $("#profile").val("").trigger("change");
    $("#users").val("").trigger("change");
    $("#first_launch").data("DateTimePicker") && $("#first_launch").data("DateTimePicker").date(moment());
    $("#flashes").empty();
};

// selectByText sets a select2 control's value to the option whose text matches
// name, used when editing (schedules reference objects by name, not id).
const selectByText = (selector, name) => {
    let match = $(selector + " option").filter(function () {
        return $(this).text() === name;
    }).first();
    if (match.length) {
        $(selector).val(match.val()).trigger("change.select2");
    }
};

const setupOptions = () => {
    api.groups.summary()
        .success((summaries) => {
            groups = summaries.groups;
            let group_s2 = $.map(groups, (obj) => {
                obj.text = obj.name;
                obj.title = obj.num_targets + " targets";
                return obj;
            });
            $("#users.form-control").select2({
                placeholder: "Select Groups",
                data: group_s2,
            });
        });
    api.templates.get()
        .success((templates) => {
            let template_s2 = $.map(templates, (obj) => {
                obj.text = obj.name;
                return obj;
            });
            $("#template.form-control").select2({
                placeholder: "Select a Template",
                data: template_s2,
            });
        });
    api.pages.get()
        .success((pages) => {
            let page_s2 = $.map(pages, (obj) => {
                obj.text = obj.name;
                return obj;
            });
            $("#page.form-control").select2({
                placeholder: "Select a Landing Page",
                data: page_s2,
            });
        });
    api.SMTP.get()
        .success((profiles) => {
            let profile_s2 = $.map(profiles, (obj) => {
                obj.text = obj.name;
                return obj;
            });
            $("#profile.form-control").select2({
                placeholder: "Select a Sending Profile",
                data: profile_s2,
            });
        });
};

const saveSchedule = (id) => {
    let groupData = $("#users").select2("data");
    let templateData = $("#template").select2("data");
    let pageData = $("#page").select2("data");
    let profileData = $("#profile").select2("data");

    let s = {
        name: $("#name").val(),
        repeat_interval: $("#repeat_interval").val(),
        url: $("#url").val(),
        send_by_offset: parseInt($("#send_by_offset").val()) || 0,
        enabled: $("#enabled").is(":checked"),
        template_name: templateData.length ? templateData[0].text : "",
        page_name: pageData.length ? pageData[0].text : "",
        smtp_name: profileData.length ? profileData[0].text : "",
        groups: $.map(groupData, (g) => {
            return { name: g.text };
        }),
    };
    let launch = $("#first_launch").val();
    s.next_launch_date = launch ? moment(launch, dateFormat).utc().format() : moment().utc().format();

    if (id != -1) {
        s.id = parseInt(id);
        api.scheduleId.put(s)
            .success((data) => {
                dismiss();
                load();
                $("#modal").modal("hide");
                successFlash(`Schedule "${escapeHtml(s.name)}" has been updated successfully!`);
            })
            .error((data) => {
                modalError(data.responseJSON.message);
            });
    } else {
        api.schedules.post(s)
            .success((data) => {
                dismiss();
                load();
                $("#modal").modal("hide");
                successFlash(`Schedule "${escapeHtml(s.name)}" has been created successfully!`);
            })
            .error((data) => {
                modalError(data.responseJSON.message);
            });
    }
};

const edit = (id) => {
    setupOptions();
    $("#modalSubmit").unbind("click").click(() => {
        saveSchedule(id);
    });
    if (id == -1) {
        $("#scheduleModalLabel").text("New Schedule");
        return;
    }
    $("#scheduleModalLabel").text("Edit Schedule");
    api.scheduleId.get(id)
        .success((s) => {
            $("#name").val(s.name);
            $("#url").val(s.url);
            $("#repeat_interval").val(s.repeat_interval);
            $("#send_by_offset").val(s.send_by_offset || "");
            $("#enabled").prop("checked", s.enabled);
            if (s.next_launch_date) {
                $("#first_launch").data("DateTimePicker").date(moment.utc(s.next_launch_date).local());
            }
            // The select2 controls are populated asynchronously; wait briefly so
            // their options exist before selecting by name.
            setTimeout(() => {
                selectByText("#template", s.template_name);
                selectByText("#page", s.page_name);
                selectByText("#profile", s.smtp_name);
                if (s.groups) {
                    let ids = [];
                    s.groups.forEach((g) => {
                        let match = $("#users option").filter(function () {
                            return $(this).text() === g.name;
                        }).first();
                        if (match.length) {
                            ids.push(match.val());
                        }
                    });
                    $("#users").val(ids).trigger("change.select2");
                }
            }, 500);
        })
        .error(() => {
            errorFlash("Error fetching schedule");
        });
};

const deleteSchedule = (id) => {
    let s = schedules.find(x => x.id == id);
    if (!s) {
        return;
    }
    Swal.fire({
        title: "Are you sure?",
        text: `This will delete the schedule '${escapeHtml(s.name)}'. Campaigns it already launched are kept.`,
        type: "warning",
        animation: false,
        showCancelButton: true,
        confirmButtonText: "Delete",
        confirmButtonColor: "#428bca",
        reverseButtons: true,
        allowOutsideClick: false,
        preConfirm: () => {
            return new Promise((resolve, reject) => {
                api.scheduleId.delete(id)
                    .success((msg) => {
                        resolve();
                    })
                    .error((data) => {
                        reject(data.responseJSON.message);
                    });
            }).catch(error => {
                Swal.showValidationMessage(error);
            });
        }
    }).then((result) => {
        if (result.value) {
            Swal.fire("Schedule Deleted!", "The schedule has been deleted.", "success");
        }
        $("button:contains('OK')").on("click", () => {
            location.reload();
        });
    });
};

const repeatLabel = (interval) => {
    switch (interval) {
        case "daily": return "Daily";
        case "weekly": return "Weekly";
        case "monthly": return "Monthly";
        default: return "Once";
    }
};

const load = () => {
    $("#scheduleTable").hide();
    $("#loading").show();
    api.schedules.get()
        .success((ss) => {
            schedules = ss;
            $("#loading").hide();
            $("#scheduleTable").show();
            let scheduleTable = $("#scheduleTable").DataTable({
                destroy: true,
                columnDefs: [{ orderable: false, targets: "no-sort" }]
            });
            scheduleTable.clear();
            $.each(schedules, (i, s) => {
                let nextRun = s.enabled && s.next_launch_date && !moment(s.next_launch_date).isSame(moment(0)) ?
                    moment.utc(s.next_launch_date).local().format(dateFormat) : "--";
                scheduleTable.row.add([
                    escapeHtml(s.name),
                    repeatLabel(s.repeat_interval),
                    nextRun,
                    s.enabled ? '<span class="label label-success">Yes</span>' : '<span class="label label-default">No</span>',
                    `
                      <div class="pull-right">
                        <button class="btn btn-primary edit_button" data-toggle="modal" data-backdrop="static" data-target="#modal" data-schedule-id="${s.id}">
                          <i class="fa fa-pencil"></i>
                        </button>
                        <button class="btn btn-danger delete_button" data-schedule-id="${s.id}">
                          <i class="fa fa-trash-o"></i>
                        </button>
                      </div>
                    `
                ]).draw();
            });
        })
        .error(() => {
            errorFlash("Error fetching schedules");
        });
};

$(document).ready(function () {
    $("#tz_note").text("Times are interpreted in your time zone (" + localZone() + ") and stored as UTC.");
    $("#first_launch").datetimepicker({
        "widgetPositioning": { "vertical": "bottom" },
        "showTodayButton": true,
        "defaultDate": moment(),
        "format": dateFormat
    });
    load();
    $("#modal").on("hide.bs.modal", function () {
        dismiss();
    });
    $("#new_button").on("click", function () {
        edit(-1);
    });
    $("#scheduleTable").on("click", ".edit_button", function (e) {
        edit($(this).attr("data-schedule-id"));
    });
    $("#scheduleTable").on("click", ".delete_button", function (e) {
        deleteSchedule($(this).attr("data-schedule-id"));
    });
});
